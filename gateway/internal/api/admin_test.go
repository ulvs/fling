package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"github.com/ulvs/fling/gateway/internal/config"
	"github.com/ulvs/fling/gateway/internal/mail"
)

const (
	adminEmail = "ada@example.com"
	password   = "a long and easy passphrase"
)

// signedInLocal invites email, sets a password through the invite link, and returns that
// person's signed-in browser.
func signedInLocal(t *testing.T, h *harness, base *browser, email string) *browser {
	t.Helper()
	b := newBrowser(t, h, base.idp)
	if page := b.submit(invite(t, h, email), url.Values{"password": {password}}); !strings.Contains(page.body, "Password saved.") {
		t.Fatalf("setting up %s: %s", email, page.body)
	}
	return b
}

// withAdmin sets up local accounts with an admin (Ada) and a regular member (Bo).
func withAdmin(t *testing.T, h *harness) (admin, member *browser) {
	t.Helper()
	base := withLocal(t, h)
	admin = signedInLocal(t, h, base, adminEmail)
	if err := h.server.Store.SetAdmin(context.Background(), userID(t, h, adminEmail), true); err != nil {
		t.Fatal(err)
	}
	member = signedInLocal(t, h, base, bo)
	return admin, member
}

func TestAdminPagesAreForAdmins(t *testing.T) {
	h := newHarness(t)
	admin, member := withAdmin(t, h)

	if page := newBrowser(t, h, admin.idp).get("/admin/people"); !strings.Contains(page.body, "Only members of this gateway can sign in") {
		t.Fatalf("signed out: %d %s", page.status, page.body)
	}
	for _, path := range []string{"/admin/people", "/admin/links", "/admin/activity"} {
		if page := member.get(path); page.status != http.StatusForbidden {
			t.Fatalf("member on %s: %d", path, page.status)
		}
	}
	if page := member.get("/"); strings.Contains(page.body, `href="/admin"`) {
		t.Fatal("members shouldn't see the Admin link")
	}
	if page := admin.get("/"); !strings.Contains(page.body, `href="/admin"`) {
		t.Fatal("admins should see the Admin link")
	}
	page := admin.get("/admin")
	if page.status != http.StatusOK || !strings.Contains(page.body, bo) || !strings.Contains(page.body, adminEmail) {
		t.Fatalf("people page: %d %s", page.status, page.body)
	}
}

func TestAdminByConfiguration(t *testing.T) {
	h := newHarness(t)
	admin, member := withAdmin(t, h)
	h.server.Admins = config.Members{Emails: []string{bo}}
	page := member.get("/admin/people")
	if page.status != http.StatusOK {
		t.Fatalf("FLING_ADMIN_EMAILS admin: %d", page.status)
	}
	if !strings.Contains(page.body, "Admin (setting)") {
		t.Fatalf("the setting should be shown: %s", page.body)
	}
	// An admin by setting can only be removed from the setting.
	if res := admin.submit("/admin/people/"+userID(t, h, bo)+"/remove-admin", url.Values{}); res.status != http.StatusConflict ||
		!strings.Contains(res.body, "FLING_ADMIN_EMAILS") {
		t.Fatalf("removing a setting admin: %d", res.status)
	}
	// Removing a database admin works.
	if res := member.submit("/admin/people/"+userID(t, h, adminEmail)+"/remove-admin", url.Values{}); !strings.Contains(res.body, "No longer an admin.") {
		t.Fatalf("removing a database admin: %d %s", res.status, res.body)
	}
	if page := admin.get("/admin/people"); page.status != http.StatusForbidden {
		t.Fatalf("removed admin still has access: %d", page.status)
	}
}

func TestAdminInvites(t *testing.T) {
	h := newHarness(t)
	admin, _ := withAdmin(t, h)

	if page := admin.submit("/admin/people/invite", url.Values{"email": {"not an email"}}); page.status != http.StatusUnprocessableEntity {
		t.Fatalf("bad email: %d", page.status)
	}

	// Without email, the link is shown to pass on.
	page := admin.submit("/admin/people/invite", url.Values{"email": {"Cy@Example.com"}, "admin": {"on"}})
	link := between(page.body, `value="https://fling.test`, `"`)
	if page.status != http.StatusOK || !strings.HasPrefix(link, "/auth/link/") {
		t.Fatalf("invite without email: %d %s", page.status, page.body)
	}
	cy := newBrowser(t, h, admin.idp)
	if page := cy.submit(link, url.Values{"password": {password}}); !strings.Contains(page.body, "Password saved.") {
		t.Fatalf("using the invite: %s", page.body)
	}
	if page := cy.get("/admin/people"); page.status != http.StatusOK {
		t.Fatalf("invited as admin, but: %d", page.status)
	}

	// With email, it's sent instead.
	fm := &fakeMail{sent: make(chan mail.Message, 1)}
	h.server.Mail = fm
	page = admin.submit("/admin/people/invite", url.Values{"email": {"dee@example.com"}})
	if !strings.Contains(page.body, "Invite sent to dee@example.com") || strings.Contains(page.body, "/auth/link/") {
		t.Fatalf("invite with email: %s", page.body)
	}
	if m := <-fm.sent; m.To != "dee@example.com" || !strings.Contains(m.Subject, "invited") {
		t.Fatalf("mail %+v", m)
	}
}

func TestAdminTurnsAccountsOffAndOn(t *testing.T) {
	h := newHarness(t)
	admin, member := withAdmin(t, h)
	boID := userID(t, h, bo)

	if res := admin.submit("/admin/people/"+boID+"/disable", url.Values{}); !strings.Contains(res.body, "Turned off and signed out everywhere") {
		t.Fatalf("disable: %d %s", res.status, res.body)
	}
	if member.signedIn() {
		t.Fatal("a turned-off account must be signed out")
	}
	if page := member.submit("/auth/password", url.Values{"email": {bo}, "password": {password}}); page.status != http.StatusUnauthorized {
		t.Fatalf("turned-off account signed in: %d", page.status)
	}
	admin.submit("/admin/people/"+boID+"/enable", url.Values{})
	if page := member.submit("/auth/password", url.Values{"email": {bo}, "password": {password}}); !strings.Contains(page.body, "Your links") {
		t.Fatalf("turned back on, but: %d", page.status)
	}
}

func TestAdminCantLockThemselvesOut(t *testing.T) {
	h := newHarness(t)
	admin, _ := withAdmin(t, h)
	self := userID(t, h, adminEmail)
	for _, action := range []string{"disable", "remove-admin"} {
		if res := admin.submit("/admin/people/"+self+"/"+action, url.Values{}); res.status != http.StatusConflict {
			t.Fatalf("%s on yourself: %d", action, res.status)
		}
	}
	if page := admin.get("/admin/people"); page.status != http.StatusOK {
		t.Fatal("still an admin")
	}
}

func TestAdminSignsSomeoneOutEverywhere(t *testing.T) {
	h := newHarness(t)
	admin, member := withAdmin(t, h)

	// Bo signs in on a computer too.
	const redirect = "http://127.0.0.1:53123/callback"
	verifier := oauth2.GenerateVerifier()
	tok := decode[tokenResponse](t, h.exchange(desktopCode(t, member, verifier, redirect, bo), verifier, redirect))

	admin.submit("/admin/people/"+userID(t, h, bo)+"/sign-out", url.Values{})
	if member.signedIn() {
		t.Fatal("browser session survived")
	}
	if rec := h.do("GET", "/api/v1/me", nil, tok.AccessToken); rec.Code != http.StatusUnauthorized {
		t.Fatalf("desktop token survived: %d", rec.Code)
	}
}

func TestAdminResetsSignIn(t *testing.T) {
	h := newHarness(t)
	admin, member := withAdmin(t, h)
	page := admin.submit("/admin/people/"+userID(t, h, bo)+"/reset", url.Values{})
	link := between(page.body, `value="https://fling.test`, `"`)
	if !strings.HasPrefix(link, "/auth/link/") {
		t.Fatalf("reset: %s", page.body)
	}
	fresh := newBrowser(t, h, admin.idp)
	if page := fresh.submit(link, url.Values{"password": {"a brand new passphrase"}}); !strings.Contains(page.body, "Password saved.") {
		t.Fatalf("using the reset link: %s", page.body)
	}
	if member.signedIn() {
		t.Fatal("old sessions must end after a reset")
	}
}

func TestAdminLinks(t *testing.T) {
	h := newHarness(t)
	admin, member := withAdmin(t, h)
	keep := h.upload("keep.txt", 10, 3600)
	stop := h.upload("Quarterly report.pdf", 20, 3600)

	page := admin.get("/admin/links?q=quarterly")
	if !strings.Contains(page.body, "Quarterly report.pdf") || strings.Contains(page.body, "keep.txt") || !strings.Contains(page.body, "dev@example.com") {
		t.Fatalf("search: %s", page.body)
	}
	if res := member.submit("/admin/links/"+stop.ID+"/revoke", url.Values{}); res.status != http.StatusForbidden {
		t.Fatalf("member revoked a link: %d", res.status)
	}
	if res := admin.visit(http.MethodPost, "/admin/links/"+stop.ID+"/revoke", url.Values{}, "cross-site"); res.status != http.StatusForbidden {
		t.Fatalf("cross-site revoke: %d", res.status)
	}
	if res := admin.submit("/admin/links/"+stop.ID+"/revoke", url.Values{"q": {"quarterly"}}); !strings.Contains(res.body, "Link turned off.") {
		t.Fatalf("revoke: %d %s", res.status, res.body)
	}
	if rec := h.do("GET", "/l/"+slugOf(stop), nil, ""); rec.Code != http.StatusGone {
		t.Fatalf("revoked link still works: %d", rec.Code)
	}
	if rec := h.do("GET", "/l/"+slugOf(keep), nil, ""); rec.Code != http.StatusFound {
		t.Fatalf("other link broke: %d", rec.Code)
	}
	if page := admin.get("/admin/links"); strings.Contains(page.body, "Quarterly report.pdf") {
		t.Fatal("turned-off links are hidden by default")
	}
	if page := admin.get("/admin/links?all=1"); !strings.Contains(page.body, "Quarterly report.pdf") {
		t.Fatal("turned-off links show with all=1")
	}
}

func TestAdminActivity(t *testing.T) {
	h := newHarness(t)
	admin, _ := withAdmin(t, h)
	admin.submit("/admin/people/"+userID(t, h, bo)+"/disable", url.Values{})
	page := admin.get("/admin/activity")
	if !strings.Contains(page.body, adminEmail+"</span> turned off <span class=\"name\">"+bo) {
		t.Fatalf("activity: %s", page.body)
	}
	if !strings.Contains(page.body, "signed in") {
		t.Fatal("sign-ins should be listed")
	}
}

func TestAdminLinkSearchIsAsTyped(t *testing.T) {
	h := newHarness(t)
	admin, _ := withAdmin(t, h)
	h.upload("Ärende 42.pdf", 10, 3600)
	h.upload("Quarterly.pdf", 10, 3600)
	for term, want := range map[string]string{
		"Ärende":    "Ärende 42.pdf", // non-ASCII capitals match as typed
		"QUARTERLY": "Quarterly.pdf", // ASCII ignores case
		"dev@":      "Quarterly.pdf", // the sharer's email
		"100%":      "",              // % is a literal, not a wildcard
	} {
		page := admin.get("/admin/links?q=" + url.QueryEscape(term))
		if want == "" {
			if strings.Contains(page.body, ".pdf</span>") {
				t.Errorf("%q matched something", term)
			}
		} else if !strings.Contains(page.body, want) {
			t.Errorf("%q didn't find %s", term, want)
		}
	}
}
