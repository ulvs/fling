package api

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/ulvs/fling/gateway/internal/auth"
	"github.com/ulvs/fling/gateway/internal/config"
	"github.com/ulvs/fling/gateway/internal/oidctest"
)

// withSignIn adds a fake identity provider and a member list (ann@example.com) to h.
func withSignIn(t *testing.T, h *harness) *oidctest.Server {
	t.Helper()
	idp := oidctest.New(t)
	h.server.Providers = auth.Providers(config.Config{
		PublicURL: h.server.PublicURL,
		Generic:   config.OIDCProvider{ClientID: idp.ClientID, ClientSecret: idp.ClientSecret, Issuer: idp.URL, Name: "Test"},
	}, idp.Client())
	h.server.Members = config.Members{Emails: []string{"ann@example.com"}}
	h.server.OrgID = "org1"
	return idp
}

// browser keeps cookies and follows redirects between the gateway (served in-process) and
// the identity provider, like a real browser would. It stops at loopback addresses, where
// the desktop app would be listening.
type browser struct {
	t       *testing.T
	h       *harness
	idp     *oidctest.Server
	cookies map[string]*http.Cookie
}

func newBrowser(t *testing.T, h *harness, idp *oidctest.Server) *browser {
	return &browser{t: t, h: h, idp: idp, cookies: map[string]*http.Cookie{}}
}

type result struct {
	status   int
	body     string
	location string // set when the chain ended at a loopback redirect
}

func (b *browser) serve(method, target string, form url.Values, site string) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method == http.MethodPost {
		req.Header.Set("Sec-Fetch-Site", site)
	}
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

// visit requests target and follows redirects until a page or a loopback redirect.
// The fake provider also listens on 127.0.0.1, so it's matched before the loopback check.
func (b *browser) visit(method, target string, form url.Values, site string) result {
	b.t.Helper()
	client := b.idp.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for range 10 {
		u, err := url.Parse(target)
		if err != nil {
			b.t.Fatal(err)
		}
		var status int
		var location, body string
		switch {
		case u.Host == "" || u.Host == "fling.test":
			rec := b.serve(method, u.RequestURI(), form, site)
			status, location, body = rec.Code, rec.Header().Get("Location"), rec.Body.String()
		case strings.HasPrefix(target, b.idp.URL):
			resp, err := client.Get(target)
			if err != nil {
				b.t.Fatal(err)
			}
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			status, location, body = resp.StatusCode, resp.Header.Get("Location"), string(raw)
		case loopbackRedirect(target):
			return result{status: http.StatusSeeOther, location: target}
		default:
			b.t.Fatalf("browser went to unexpected %s", target)
		}
		if status < 300 || status >= 400 {
			return result{status: status, body: body}
		}
		target, method, form = location, http.MethodGet, nil
	}
	b.t.Fatal("too many redirects")
	return result{}
}

func (b *browser) get(target string) result { return b.visit(http.MethodGet, target, nil, "") }

func (b *browser) post(target string) result {
	return b.visit(http.MethodPost, target, url.Values{}, "same-origin")
}

func TestBrowserSignInAndOut(t *testing.T) {
	h := newHarness(t)
	idp := withSignIn(t, h)
	b := newBrowser(t, h, idp)

	page := b.get("/")
	if page.status != http.StatusOK || !strings.Contains(page.body, `href="/auth/login/oidc"`) {
		t.Fatalf("sign-in page: %d %s", page.status, page.body)
	}
	page = b.get("/auth/login/oidc")
	if page.status != http.StatusOK || !strings.Contains(page.body, "ann@example.com") || !strings.Contains(page.body, "Your links") {
		t.Fatalf("after sign-in: %d %s", page.status, page.body)
	}
	session := b.cookies["__Host-fling_session"]
	if session == nil || !session.Secure || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode || session.Path != "/" {
		t.Fatalf("session cookie %+v", session)
	}
	if _, ok := b.cookies["__Host-fling_login"]; ok {
		t.Error("login cookie should be cleared after the callback")
	}

	if page = b.post("/auth/logout"); !strings.Contains(page.body, "Continue with Test") {
		t.Fatalf("after sign-out: %s", page.body)
	}
	// The old cookie value no longer works, even if someone kept it.
	b.cookies["__Host-fling_session"] = session
	if page = b.get("/"); strings.Contains(page.body, "Your links") {
		t.Fatal("session still valid after sign-out")
	}
}

func TestSignInRefusals(t *testing.T) {
	h := newHarness(t)
	idp := withSignIn(t, h)

	idp.SignInAs("eve@example.com", true)
	b := newBrowser(t, h, idp)
	if page := b.get("/auth/login/oidc"); page.status != http.StatusForbidden || !strings.Contains(page.body, "member") {
		t.Fatalf("non-member: %d %s", page.status, page.body)
	}
	if b.cookies["__Host-fling_session"] != nil {
		t.Fatal("non-member got a session")
	}

	idp.SignInAs("ann@example.com", false)
	b = newBrowser(t, h, idp)
	if page := b.get("/auth/login/oidc"); page.status != http.StatusForbidden || !strings.Contains(page.body, "verified email") {
		t.Fatalf("unverified: %d %s", page.status, page.body)
	}

	if page := newBrowser(t, h, idp).get("/auth/login/nope"); page.status != http.StatusNotFound {
		t.Fatalf("unknown provider: %d", page.status)
	}
}

// A callback only completes in the browser that started the sign-in (login CSRF).
func TestCallbackNeedsTheSameBrowser(t *testing.T) {
	h := newHarness(t)
	idp := withSignIn(t, h)
	attacker := newBrowser(t, h, idp)

	rec := attacker.serve("GET", "/auth/login/oidc", nil, "")
	client := idp.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Get(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	callback := resp.Header.Get("Location")

	victim := newBrowser(t, h, idp)
	if page := victim.get(callback); page.status != http.StatusBadRequest {
		t.Fatalf("callback in another browser: %d %s", page.status, page.body)
	}
	if victim.cookies["__Host-fling_session"] != nil {
		t.Fatal("victim was signed in")
	}
}

// desktopStart is what Fling on Windows opens in the browser.
func desktopStart(verifier, redirect string) string {
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"redirect_uri": {redirect}, "state": {"client-state"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
	}
	return "/auth/desktop?" + q.Encode()
}

func (h *harness) exchange(code, verifier, redirect string) *httptest.ResponseRecorder {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirect}}
	req := httptest.NewRequest("POST", "/api/v1/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

// desktopCode runs the browser part of a desktop sign-in and returns the one-time code.
func desktopCode(t *testing.T, b *browser, verifier, redirect string) string {
	t.Helper()
	page := b.get(desktopStart(verifier, redirect))
	if page.status != http.StatusOK {
		t.Fatalf("desktop start: %d %s", page.status, page.body)
	}
	if strings.Contains(page.body, "Continue with Test") {
		// Not signed in yet: the sign-in page carries the flow along.
		flow := page.body[strings.Index(page.body, "/auth/login/oidc?flow="):]
		flow = strings.ReplaceAll(flow[:strings.Index(flow, `"`)], "&amp;", "&")
		page = b.get(flow)
	}
	if !strings.Contains(page.body, "will share files as <strong>ann@example.com</strong>") {
		t.Fatalf("confirm page: %d %s", page.status, page.body)
	}
	form := page.body[strings.Index(page.body, `action="`)+8:]
	done := b.post(form[:strings.Index(form, `"`)])
	back, err := url.Parse(done.location)
	if err != nil || done.location == "" || !strings.HasPrefix(done.location, redirect) {
		t.Fatalf("expected redirect to the app, got %+v", done)
	}
	if back.Query().Get("state") != "client-state" {
		t.Fatalf("state not passed back: %s", done.location)
	}
	return back.Query().Get("code")
}

func TestDesktopSignIn(t *testing.T) {
	h := newHarness(t)
	idp := withSignIn(t, h)
	b := newBrowser(t, h, idp)
	const redirect = "http://127.0.0.1:53123/callback"
	verifier := oauth2.GenerateVerifier()
	code := desktopCode(t, b, verifier, redirect)

	rec := h.exchange(code, verifier, redirect)
	if rec.Code != http.StatusOK {
		t.Fatalf("token: %d %s", rec.Code, rec.Body)
	}
	tok := decode[tokenResponse](t, rec)
	if tok.TokenType != "Bearer" || tok.Email != "ann@example.com" || tok.AccessToken == "" || tok.ExpiresIn != int64(desktopSessionTTL/time.Second) {
		t.Fatalf("token response %+v", tok)
	}
	if rec := h.exchange(code, verifier, redirect); rec.Code != http.StatusBadRequest {
		t.Fatal("a code must only work once")
	}

	if rec := h.do("GET", "/api/v1/me", nil, tok.AccessToken); rec.Code != http.StatusOK || decode[map[string]string](t, rec)["email"] != "ann@example.com" {
		t.Fatalf("me: %d %s", rec.Code, rec.Body)
	}
	rec = h.do("POST", "/api/v1/uploads", map[string]any{"name": "a.txt", "size": 5}, tok.AccessToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload with desktop token: %d %s", rec.Code, rec.Body)
	}

	// The second desktop sign-in reuses the browser session and only asks to confirm.
	verifier2 := oauth2.GenerateVerifier()
	if code2 := desktopCode(t, b, verifier2, redirect); h.exchange(code2, verifier2, redirect).Code != http.StatusOK {
		t.Fatal("second desktop sign-in failed")
	}

	if rec := h.do("DELETE", "/api/v1/session", nil, tok.AccessToken); rec.Code != http.StatusNoContent {
		t.Fatalf("sign out: %d", rec.Code)
	}
	if rec := h.do("GET", "/api/v1/me", nil, tok.AccessToken); rec.Code != http.StatusUnauthorized {
		t.Fatalf("token still works after sign-out: %d", rec.Code)
	}
}

func TestDesktopTokenExchangeChecks(t *testing.T) {
	h := newHarness(t)
	idp := withSignIn(t, h)
	b := newBrowser(t, h, idp)
	const redirect = "http://127.0.0.1:53123/callback"

	verifier := oauth2.GenerateVerifier()
	code := desktopCode(t, b, verifier, redirect)
	if rec := h.exchange(code, oauth2.GenerateVerifier(), redirect); rec.Code != http.StatusBadRequest {
		t.Fatal("wrong verifier accepted")
	}

	code = desktopCode(t, b, verifier, redirect)
	if rec := h.exchange(code, verifier, "http://127.0.0.1:9999/other"); rec.Code != http.StatusBadRequest {
		t.Fatal("different redirect_uri accepted")
	}

	code = desktopCode(t, b, verifier, redirect)
	h.advance(desktopCodeTTL + time.Second)
	if rec := h.exchange(code, verifier, redirect); rec.Code != http.StatusBadRequest {
		t.Fatal("expired code accepted")
	}
}

func TestDesktopStartValidation(t *testing.T) {
	h := newHarness(t)
	idp := withSignIn(t, h)
	verifier := oauth2.GenerateVerifier()
	for redirect, ok := range map[string]bool{
		"http://127.0.0.1:53123/callback": true,
		"http://[::1]:53123/":             true,
		"http://localhost:5000/cb":        true,
		"https://evil.example/cb":         false,
		"http://evil.example:80/cb":       false,
		"http://127.0.0.1/cb":             false, // no port
		"http://user@127.0.0.1:5000/cb":   false,
		"http://127.0.0.1.evil.io:5/cb":   false,
	} {
		page := newBrowser(t, h, idp).get(desktopStart(verifier, redirect))
		if got := page.status == http.StatusOK; got != ok {
			t.Errorf("%s: status %d", redirect, page.status)
		}
	}
	if page := newBrowser(t, h, idp).get("/auth/desktop?redirect_uri=http://127.0.0.1:5/&state=x&code_challenge_method=plain&code_challenge=abc"); page.status != http.StatusBadRequest {
		t.Errorf("plain PKCE accepted: %d", page.status)
	}
}

// Approving needs a same-origin POST, so another site can't approve on the user's behalf.
func TestDesktopApproveRejectsCrossSite(t *testing.T) {
	h := newHarness(t)
	idp := withSignIn(t, h)
	b := newBrowser(t, h, idp)
	b.get("/auth/login/oidc")

	page := b.get(desktopStart(oauth2.GenerateVerifier(), "http://127.0.0.1:53123/cb"))
	form := page.body[strings.Index(page.body, `action="`)+8:]
	action := form[:strings.Index(form, `"`)]
	if res := b.visit(http.MethodPost, action, url.Values{}, "cross-site"); res.status != http.StatusForbidden {
		t.Fatalf("cross-site approve: %+v", res)
	}
}

// Taking someone off the member list ends their access, even with a valid token.
func TestRemovedMemberLosesAccess(t *testing.T) {
	h := newHarness(t)
	idp := withSignIn(t, h)
	b := newBrowser(t, h, idp)
	const redirect = "http://127.0.0.1:53123/callback"
	verifier := oauth2.GenerateVerifier()
	tok := decode[tokenResponse](t, h.exchange(desktopCode(t, b, verifier, redirect), verifier, redirect))

	h.server.Members = config.Members{}
	if rec := h.do("GET", "/api/v1/me", nil, tok.AccessToken); rec.Code != http.StatusUnauthorized {
		t.Fatalf("removed member still has API access: %d", rec.Code)
	}
	if page := b.get("/"); strings.Contains(page.body, "Your links") {
		t.Fatal("removed member still has a browser session")
	}
}

// Desktop tokens slide forward when used, so a computer in daily use stays signed in.
func TestDesktopSessionSlides(t *testing.T) {
	h := newHarness(t)
	idp := withSignIn(t, h)
	b := newBrowser(t, h, idp)
	const redirect = "http://127.0.0.1:53123/callback"
	verifier := oauth2.GenerateVerifier()
	tok := decode[tokenResponse](t, h.exchange(desktopCode(t, b, verifier, redirect), verifier, redirect))

	for range 4 {
		h.advance(60 * 24 * time.Hour)
		if rec := h.do("GET", "/api/v1/me", nil, tok.AccessToken); rec.Code != http.StatusOK {
			t.Fatalf("token expired despite use: %d", rec.Code)
		}
	}
	h.advance(desktopSessionTTL + time.Hour)
	if rec := h.do("GET", "/api/v1/me", nil, tok.AccessToken); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unused token should expire: %d", rec.Code)
	}
}
