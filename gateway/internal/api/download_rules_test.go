package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestMembersOnlyDownloads(t *testing.T) {
	h := newHarness(t)
	admin, _ := withAdmin(t, h)
	link := h.upload("plan.pdf", 10, 3600)
	slug := slugOf(link)

	if page := admin.submit("/admin/rules", url.Values{"download": {"members"}}); !strings.Contains(page.body, "Only signed-in members can download.") {
		t.Fatalf("saving the rule: %s", page.body)
	}

	// A stranger gets the sign-in page instead of the file.
	visitor := newBrowser(t, h, admin.idp)
	rec := visitor.serve("GET", "/l/"+slug, nil, "")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Sign in to download") {
		t.Fatalf("anonymous download: %d %s", rec.Code, rec.Body)
	}

	// After signing in, the browser goes straight back to the download.
	rec = visitor.send("POST", "/auth/password", "application/x-www-form-urlencoded",
		[]byte(url.Values{"email": {bo}, "password": {password}}.Encode()))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sign-in: %d %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Location"); got != "/l/"+slug {
		t.Fatalf("after sign-in went to %q", got)
	}
	rec = visitor.serve("GET", "/l/"+slug, nil, "")
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "https://s3.test/get/") {
		t.Fatalf("signed-in download: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// The return address is used once.
	if rec := visitor.serve("GET", "/", nil, ""); !strings.Contains(rec.Body.String(), "Your links") {
		t.Fatal("start page should work normally again")
	}
	if page := admin.get("/admin/activity"); !strings.Contains(page.body, bo+"</span> downloaded a file") {
		t.Fatalf("activity should name the downloader: %s", page.body)
	}

	// Back to "anyone": no sign-in needed.
	admin.submit("/admin/rules", url.Values{"download": {"anyone"}})
	if rec := newBrowser(t, h, admin.idp).serve("GET", "/l/"+slug, nil, ""); rec.Code != http.StatusFound {
		t.Fatalf("anyone-with-the-link download: %d", rec.Code)
	}
}

func TestMembersOnlyWithPasskey(t *testing.T) {
	h := newHarness(t)
	admin, _ := withAdmin(t, h)
	pk, _ := registerPasskey(t, newBrowser(t, h, admin.idp), strings.TrimPrefix(invite(t, h, "cy@example.com"), "/auth/link/"))
	admin.submit("/admin/rules", url.Values{"download": {"members"}})
	slug := slugOf(h.upload("plan.pdf", 10, 3600))

	visitor := newBrowser(t, h, admin.idp)
	visitor.serve("GET", "/l/"+slug, nil, "")
	rec := pk.signIn(t, visitor, "")
	if got := decode[map[string]string](t, rec)["redirect"]; got != "/l/"+slug {
		t.Fatalf("passkey sign-in went to %q", got)
	}
}

func TestDownloadLimit(t *testing.T) {
	h := newHarness(t)
	admin, _ := withAdmin(t, h)
	admin.submit("/admin/rules", url.Values{"max_downloads": {"2"}})
	slug := slugOf(h.upload("plan.pdf", 10, 3600))
	for i := 1; i <= 2; i++ {
		if rec := h.do("GET", "/l/"+slug, nil, ""); rec.Code != http.StatusFound {
			t.Fatalf("download %d: %d", i, rec.Code)
		}
	}
	rec := h.do("GET", "/l/"+slug, nil, "")
	if rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "used up") {
		t.Fatalf("third download: %d %s", rec.Code, rec.Body)
	}
	if page := admin.submit("/admin/rules", url.Values{"max_downloads": {"0"}}); page.status != http.StatusUnprocessableEntity {
		t.Fatalf("0 downloads accepted: %d", page.status)
	}
}

func TestReturnAddressIsOnlyADownloadLink(t *testing.T) {
	for path, ok := range map[string]bool{
		"/l/AbC-_123":                    true,
		"/l/":                            false,
		"https://evil.example":           false,
		"//evil.example/l/x":             false,
		"/l/x/../../admin":               false,
		"/l/x?next=//evil":               false,
		"/admin":                         false,
		"/l/" + strings.Repeat("a", 200): false,
	} {
		if got := validReturn(path); got != ok {
			t.Errorf("%q: %v", path, got)
		}
	}

	h := newHarness(t)
	_, member := withAdmin(t, h)
	member.post("/auth/logout")
	member.cookies["__Host-"+returnCookie] = &http.Cookie{Name: "__Host-" + returnCookie, Value: "https://evil.example"}
	rec := member.send("POST", "/auth/password", "application/x-www-form-urlencoded",
		[]byte(url.Values{"email": {bo}, "password": {password}}.Encode()))
	if got := rec.Header().Get("Location"); got != "/" {
		t.Fatalf("a forged return address was followed: %q", got)
	}
}

// Simultaneous requests can't both take the last download.
func TestDownloadLimitUnderConcurrency(t *testing.T) {
	h := newHarness(t)
	admin, _ := withAdmin(t, h)
	admin.submit("/admin/rules", url.Values{"max_downloads": {"1"}})
	slug := slugOf(h.upload("plan.pdf", 10, 3600))

	// A link scanner looking with HEAD doesn't use the download up.
	req := httptest.NewRequest("HEAD", "/l/"+slug, nil)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD: %d", rec.Code)
	}

	const n = 20
	codes := make(chan int, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.handler.ServeHTTP(rec, httptest.NewRequest("GET", "/l/"+slug, nil))
			codes <- rec.Code
		}()
	}
	wg.Wait()
	close(codes)
	served := 0
	for c := range codes {
		if c == http.StatusFound {
			served++
		} else if c != http.StatusGone {
			t.Errorf("unexpected status %d", c)
		}
	}
	if served != 1 {
		t.Fatalf("a 1-download link was served %d times", served)
	}
}
