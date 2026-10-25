package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/descope/virtualwebauthn"

	"github.com/ulvs/fling/gateway/internal/auth"
	"github.com/ulvs/fling/gateway/internal/config"
	"github.com/ulvs/fling/gateway/internal/mail"
	"github.com/ulvs/fling/gateway/internal/oidctest"
)

func init() { auth.UseCheapHashingForTests() }

const bo = "bo@example.com" // a local account; not on the member list

type fakeMail struct{ sent chan mail.Message }

func (f *fakeMail) Send(_ context.Context, m mail.Message) error {
	f.sent <- m
	return nil
}

// withLocal turns on local accounts and passkeys, and returns a browser for h.
func withLocal(t *testing.T, h *harness) *browser {
	t.Helper()
	wa, err := NewWebAuthn("https://fling.test", "fling.test")
	if err != nil {
		t.Fatal(err)
	}
	h.server.Local, h.server.WebAuthn, h.server.OrgID = true, wa, "org1"
	h.server.Members = config.Members{}
	return newBrowser(t, h, oidctest.New(t))
}

// invite creates a local account for email and returns its invite path.
func invite(t *testing.T, h *harness, email string) string {
	t.Helper()
	u, err := h.server.Store.EnsureLocalUser(context.Background(), "org1", email, *h.now.Load())
	if err != nil {
		t.Fatal(err)
	}
	link, sent, err := h.server.CreateAccountLink(context.Background(), u, "invite", InviteTTL)
	if err != nil || sent {
		t.Fatalf("link %q sent=%v err=%v", link, sent, err)
	}
	return strings.TrimPrefix(link, h.server.PublicURL)
}

func (b *browser) send(method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for _, c := range b.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	b.h.handler.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	return rec
}

func (b *browser) postJSON(path string, body any) *httptest.ResponseRecorder {
	data, ok := body.(string)
	if !ok {
		raw, _ := json.Marshal(body)
		data = string(raw)
	}
	return b.send("POST", path, "application/json", []byte(data))
}

func (b *browser) submit(path string, form url.Values) result {
	return b.visit(http.MethodPost, path, form, "same-origin")
}

func (b *browser) signedIn() bool { return strings.Contains(b.get("/").body, "Your links") }

func between(body, start, end string) string {
	i := strings.Index(body, start)
	if i < 0 {
		return ""
	}
	rest := body[i+len(start):]
	return rest[:strings.Index(rest, end)]
}

func TestInviteWithPassword(t *testing.T) {
	h := newHarness(t)
	b := withLocal(t, h)
	link := invite(t, h, bo)

	if page := b.get(link); page.status != http.StatusOK || !strings.Contains(page.body, "Welcome to Fling") || !strings.Contains(page.body, bo) {
		t.Fatalf("invite page: %d %s", page.status, page.body)
	}
	if page := b.submit(link, url.Values{"password": {"too short"}}); page.status != http.StatusUnprocessableEntity {
		t.Fatalf("short password: %d", page.status)
	}
	if page := b.submit(link, url.Values{"password": {"a long and easy passphrase"}}); !strings.Contains(page.body, "Password saved.") {
		t.Fatalf("after setting password: %d %s", page.status, page.body)
	}
	if !b.signedIn() {
		t.Fatal("not signed in after accepting the invite")
	}
	if page := newBrowser(t, h, b.idp).get(link); page.status != http.StatusGone {
		t.Fatalf("invite link works twice: %d", page.status)
	}

	other := newBrowser(t, h, b.idp)
	if page := other.submit("/auth/password", url.Values{"email": {"BO@example.com"}, "password": {"wrong password!!"}}); page.status != http.StatusUnauthorized ||
		!strings.Contains(page.body, "don&#39;t match") {
		t.Fatalf("wrong password: %d %s", page.status, page.body)
	}
	if page := other.submit("/auth/password", url.Values{"email": {"BO@example.com"}, "password": {"a long and easy passphrase"}}); !strings.Contains(page.body, "Your links") {
		t.Fatalf("password sign-in: %d %s", page.status, page.body)
	}
	if page := newBrowser(t, h, b.idp).submit("/auth/password", url.Values{"email": {"nobody@example.com"}, "password": {"a long and easy passphrase"}}); page.status != http.StatusUnauthorized {
		t.Fatalf("unknown account: %d", page.status)
	}
}

func TestPasswordThrottle(t *testing.T) {
	h := newHarness(t)
	b := withLocal(t, h)
	b.submit(invite(t, h, bo), url.Values{"password": {"a long and easy passphrase"}})

	attacker := newBrowser(t, h, b.idp)
	for range 5 {
		attacker.submit("/auth/password", url.Values{"email": {bo}, "password": {"guess guess guess"}})
	}
	if page := attacker.submit("/auth/password", url.Values{"email": {bo}, "password": {"a long and easy passphrase"}}); page.status != http.StatusTooManyRequests {
		t.Fatalf("6th attempt: %d", page.status)
	}
	h.advance(16 * time.Minute)
	if page := attacker.submit("/auth/password", url.Values{"email": {bo}, "password": {"a long and easy passphrase"}}); !strings.Contains(page.body, "Your links") {
		t.Fatalf("after the window: %d", page.status)
	}
}

func TestDisabledAccountCantSignIn(t *testing.T) {
	h := newHarness(t)
	b := withLocal(t, h)
	b.submit(invite(t, h, bo), url.Values{"password": {"a long and easy passphrase"}})
	acct, _ := h.server.Store.AccountByEmail(context.Background(), "org1", bo)
	if err := h.server.Store.SetDisabled(context.Background(), acct.ID, true, *h.now.Load()); err != nil {
		t.Fatal(err)
	}
	if b.signedIn() {
		t.Fatal("disabling must end the session")
	}
	if page := b.submit("/auth/password", url.Values{"email": {bo}, "password": {"a long and easy passphrase"}}); page.status != http.StatusUnauthorized {
		t.Fatalf("disabled account signed in: %d", page.status)
	}
}

// setUpCodes turns on two-step codes from the setup page and returns the secret and recovery codes.
func setUpCodes(t *testing.T, h *harness, b *browser) (string, []string) {
	t.Helper()
	page := b.get("/auth/code/setup")
	secret := between(page.body, `Enter this key in the app: <span class="meta">`, "<")
	if secret == "" || !strings.Contains(page.body, `<svg class="qr"`) {
		t.Fatalf("setup page: %d %s", page.status, page.body)
	}
	if page := b.submit("/auth/code/setup", url.Values{"code": {"000000"}}); page.status != http.StatusUnauthorized {
		t.Fatalf("wrong setup code: %d", page.status)
	}
	code, _ := auth.TOTPCode(secret, *h.now.Load())
	page = b.submit("/auth/code/setup", url.Values{"code": {code}})
	codes := strings.Split(between(page.body, `<ol class="codes"><li>`, "</li></ol>"), "</li><li>")
	if len(codes) != 10 {
		t.Fatalf("recovery codes page: %d %s", page.status, page.body)
	}
	return secret, codes
}

func TestTwoStepCodes(t *testing.T) {
	h := newHarness(t)
	b := withLocal(t, h)
	b.submit(invite(t, h, bo), url.Values{"password": {"a long and easy passphrase"}})
	secret, recovery := setUpCodes(t, h, b)
	b.post("/auth/logout")

	signIn := func() result {
		return b.submit("/auth/password", url.Values{"email": {bo}, "password": {"a long and easy passphrase"}})
	}
	if page := signIn(); !strings.Contains(page.body, "Enter your code") || b.signedIn() {
		t.Fatalf("password alone signed in: %s", page.body)
	}
	// The code used during setup can't be replayed.
	setupCode, _ := auth.TOTPCode(secret, *h.now.Load())
	if page := b.submit("/auth/code", url.Values{"code": {setupCode}}); page.status != http.StatusUnauthorized {
		t.Fatalf("replayed code: %d", page.status)
	}
	h.advance(30 * time.Second)
	code, _ := auth.TOTPCode(secret, *h.now.Load())
	if page := b.submit("/auth/code", url.Values{"code": {code}}); !strings.Contains(page.body, "Your links") {
		t.Fatalf("code sign-in: %d %s", page.status, page.body)
	}

	b.post("/auth/logout")
	signIn()
	if page := b.submit("/auth/code", url.Values{"code": {strings.ToUpper(recovery[0])}}); !strings.Contains(page.body, "Your links") {
		t.Fatalf("recovery code sign-in: %d %s", page.status, page.body)
	}
	b.post("/auth/logout")
	signIn()
	if page := b.submit("/auth/code", url.Values{"code": {recovery[0]}}); page.status != http.StatusUnauthorized {
		t.Fatal("a recovery code worked twice")
	}

	// Five wrong codes end the half-done sign-in.
	b.post("/auth/logout")
	signIn()
	for range 5 {
		b.submit("/auth/code", url.Values{"code": {"123456"}})
	}
	h.advance(30 * time.Second)
	code, _ = auth.TOTPCode(secret, *h.now.Load())
	if page := b.submit("/auth/code", url.Values{"code": {code}}); page.status == http.StatusOK {
		t.Fatal("sign-in survived five wrong codes")
	}
}

func TestRequireTwoFactor(t *testing.T) {
	h := newHarness(t)
	b := withLocal(t, h)
	h.server.RequireTwoFactor = true
	if page := b.submit(invite(t, h, bo), url.Values{"password": {"a long and easy passphrase"}}); !strings.Contains(page.body, "Set up two-step sign-in") {
		t.Fatalf("invite with required codes: %s", page.body)
	}
	if b.signedIn() {
		t.Fatal("signed in before setting up codes")
	}
	setUpCodes(t, h, b)
	if !b.signedIn() {
		t.Fatal("not signed in after setting up codes")
	}
	if page := b.submit("/account/code/off", url.Values{"code": {"x"}}); page.status != http.StatusForbidden {
		t.Fatalf("turned off required codes: %d", page.status)
	}
}

// passkey is a software authenticator holding one credential.
type passkey struct {
	rp   virtualwebauthn.RelyingParty
	auth virtualwebauthn.Authenticator
	cred virtualwebauthn.Credential
}

func registerPasskey(t *testing.T, b *browser, link string) (*passkey, *httptest.ResponseRecorder) {
	t.Helper()
	begin := b.postJSON("/auth/passkey/register/begin", map[string]string{"link": link})
	if begin.Code != http.StatusOK {
		t.Fatalf("register begin: %d %s", begin.Code, begin.Body)
	}
	opts, err := virtualwebauthn.ParseAttestationOptions(begin.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	pk := &passkey{
		rp:   virtualwebauthn.RelyingParty{ID: "fling.test", Name: "Fling", Origin: "https://fling.test"},
		auth: virtualwebauthn.NewAuthenticatorWithOptions(virtualwebauthn.AuthenticatorOptions{UserHandle: []byte(opts.UserID)}),
		cred: virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2),
	}
	pk.auth.AddCredential(pk.cred)
	return pk, b.postJSON("/auth/passkey/register/finish", virtualwebauthn.CreateAttestationResponse(pk.rp, pk.auth, pk.cred, *opts))
}

func (pk *passkey) signIn(t *testing.T, b *browser, flow string) *httptest.ResponseRecorder {
	t.Helper()
	begin := b.postJSON("/auth/passkey/login/begin", map[string]string{"flow": flow})
	if begin.Code != http.StatusOK {
		t.Fatalf("login begin: %d %s", begin.Code, begin.Body)
	}
	opts, err := virtualwebauthn.ParseAssertionOptions(begin.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	return b.postJSON("/auth/passkey/login/finish", virtualwebauthn.CreateAssertionResponse(pk.rp, pk.auth, pk.cred, *opts))
}

func TestPasskeyInviteAndSignIn(t *testing.T) {
	h := newHarness(t)
	b := withLocal(t, h)
	link := invite(t, h, bo)
	token := strings.TrimPrefix(link, "/auth/link/")

	pk, done := registerPasskey(t, b, token)
	if done.Code != http.StatusOK || decode[map[string]string](t, done)["redirect"] != "/account?notice=passkey" {
		t.Fatalf("register finish: %d %s", done.Code, done.Body)
	}
	if !b.signedIn() {
		t.Fatal("not signed in after creating a passkey from the invite")
	}
	if page := b.get("/account"); !strings.Contains(page.body, "Passkey added") {
		t.Fatalf("account page should list the passkey: %s", page.body)
	}
	if page := newBrowser(t, h, b.idp).get(link); page.status != http.StatusGone {
		t.Fatal("invite still works after creating a passkey")
	}

	other := newBrowser(t, h, b.idp)
	rec := pk.signIn(t, other, "")
	if rec.Code != http.StatusOK || decode[map[string]string](t, rec)["redirect"] != "/" || !other.signedIn() {
		t.Fatalf("passkey sign-in: %d %s", rec.Code, rec.Body)
	}

	// A ceremony works once.
	begin := other.postJSON("/auth/passkey/login/begin", map[string]string{})
	opts, _ := virtualwebauthn.ParseAssertionOptions(begin.Body.String())
	answer := virtualwebauthn.CreateAssertionResponse(pk.rp, pk.auth, pk.cred, *opts)
	other.postJSON("/auth/passkey/login/finish", answer)
	if rec := other.postJSON("/auth/passkey/login/finish", answer); rec.Code != http.StatusGone {
		t.Fatalf("replayed passkey answer: %d", rec.Code)
	}

	// The only sign-in method can't be removed.
	keys, _ := h.server.Store.Passkeys(context.Background(), userID(t, h, bo))
	if page := b.submit("/account/passkeys/"+keys[0].ID+"/remove", url.Values{}); page.status != http.StatusConflict {
		t.Fatalf("removed the last sign-in method: %d", page.status)
	}

	// A disabled account's passkey stops working.
	h.server.Store.SetDisabled(context.Background(), userID(t, h, bo), true, *h.now.Load())
	if rec := pk.signIn(t, newBrowser(t, h, b.idp), ""); rec.Code != http.StatusForbidden {
		t.Fatalf("disabled account's passkey: %d", rec.Code)
	}
}

func TestPasskeyRejectsOtherOriginsAndUnknownKeys(t *testing.T) {
	h := newHarness(t)
	b := withLocal(t, h)
	pk, _ := registerPasskey(t, b, strings.TrimPrefix(invite(t, h, bo), "/auth/link/"))

	phish := *pk
	phish.rp.Origin = "https://fling.test.evil.example"
	if rec := phish.signIn(t, newBrowser(t, h, b.idp), ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("answer for another origin accepted: %d", rec.Code)
	}
	stranger := *pk
	stranger.cred = virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	stranger.auth = virtualwebauthn.NewAuthenticatorWithOptions(pk.auth.Options)
	stranger.auth.AddCredential(stranger.cred)
	if rec := stranger.signIn(t, newBrowser(t, h, b.idp), ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unregistered key accepted: %d", rec.Code)
	}
}

func TestPasskeySignInForDesktop(t *testing.T) {
	h := newHarness(t)
	b := withLocal(t, h)
	pk, _ := registerPasskey(t, b, strings.TrimPrefix(invite(t, h, bo), "/auth/link/"))

	fresh := newBrowser(t, h, b.idp)
	page := fresh.get(desktopStart("verifier-verifier-verifier-verifier-verifier", "http://127.0.0.1:5000/cb"))
	flow := between(page.body, `data-flow="`, `"`)
	if flow == "" {
		t.Fatalf("desktop sign-in page has no passkey button: %s", page.body)
	}
	rec := pk.signIn(t, fresh, flow)
	if got := decode[map[string]string](t, rec)["redirect"]; got != "/auth/desktop/"+flow {
		t.Fatalf("redirect %q", got)
	}
}

func TestForgotPassword(t *testing.T) {
	h := newHarness(t)
	b := withLocal(t, h)
	b.submit(invite(t, h, bo), url.Values{"password": {"a long and easy passphrase"}})
	fm := &fakeMail{sent: make(chan mail.Message, 1)}
	h.server.Mail = fm

	if page := b.get("/"); !strings.Contains(newBrowser(t, h, b.idp).get("/").body, "Forgot your password?") {
		t.Fatalf("no forgot link: %s", page.body)
	}
	for _, email := range []string{"nobody@example.com", bo} {
		page := newBrowser(t, h, b.idp).submit("/auth/forgot", url.Values{"email": {email}})
		if !strings.Contains(page.body, "If there&#39;s an account for that address") {
			t.Fatalf("forgot response for %s: %s", email, page.body)
		}
	}
	var m mail.Message
	select {
	case m = <-fm.sent:
	case <-time.After(5 * time.Second):
		t.Fatal("no reset email")
	}
	if m.To != bo || !strings.Contains(m.Subject, "Reset") {
		t.Fatalf("unexpected mail %+v", m)
	}
	link := "/auth/link/" + between(m.Text, "/auth/link/", "\n")
	fresh := newBrowser(t, h, b.idp)
	if page := fresh.submit(link, url.Values{"password": {"another long passphrase"}}); !strings.Contains(page.body, "Password saved.") {
		t.Fatalf("reset: %s", page.body)
	}
	if b.signedIn() {
		t.Fatal("a reset must end the old sessions")
	}
	select {
	case extra := <-fm.sent:
		t.Fatalf("mail sent for an unknown address: %+v", extra)
	default:
	}
}

func TestClientIP(t *testing.T) {
	s := &Server{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	for _, tc := range []struct{ remote, xff, want string }{
		{"203.0.113.9:4000", "", "203.0.113.9"},
		{"203.0.113.9:4000", "1.2.3.4", "203.0.113.9"},             // not a proxy: header ignored
		{"10.0.0.5:4000", "198.51.100.7", "198.51.100.7"},          // via the proxy
		{"10.0.0.5:4000", "6.6.6.6, 198.51.100.7", "198.51.100.7"}, // a client can't push its own entry
		{"10.0.0.5:4000", "198.51.100.7, 10.0.0.9", "198.51.100.7"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		if got := s.clientIP(r); got != tc.want {
			t.Errorf("%s / %q: got %s, want %s", tc.remote, tc.xff, got, tc.want)
		}
	}
}

func userID(t *testing.T, h *harness, email string) string {
	a, err := h.server.Store.AccountByEmail(context.Background(), "org1", email)
	if err != nil {
		t.Fatal(err)
	}
	return a.ID
}
