package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// tryUpload announces an upload with the dev token and returns the status and error code.
func (h *harness) tryUpload(name string, size, ttlSeconds int64) (int, string) {
	h.t.Helper()
	rec := h.do("POST", "/api/v1/uploads", map[string]any{"name": name, "size": size, "ttlSeconds": ttlSeconds}, testToken)
	if rec.Code == http.StatusCreated {
		return rec.Code, ""
	}
	return rec.Code, errorCode(h.t, rec)
}

func TestRulesAreForAdmins(t *testing.T) {
	h := newHarness(t)
	_, member := withAdmin(t, h)
	if page := member.get("/admin/rules"); page.status != http.StatusForbidden {
		t.Fatalf("member saw rules: %d", page.status)
	}
	if res := member.submit("/admin/rules", url.Values{"max_mb": {"1"}}); res.status != http.StatusForbidden {
		t.Fatalf("member changed rules: %d", res.status)
	}
}

func TestRulesApplyToUploads(t *testing.T) {
	h := newHarness(t)
	admin, _ := withAdmin(t, h)
	h.server.Policy.MaxUploadBytes = 10 << 20 // server setting: 10 MB

	if code, _ := h.tryUpload("big.bin", 2<<20, 0); code != http.StatusCreated {
		t.Fatalf("2 MB under the server setting: %d", code)
	}

	page := admin.submit("/admin/rules", url.Values{
		"default_days": {"2"}, "max_days": {"3"}, "max_mb": {"1"},
		"blocked": {"EXE, .bat"}, "allowed": {""},
	})
	if !strings.Contains(page.body, "Rules saved.") || !strings.Contains(page.body, `value=".exe, .bat"`) {
		t.Fatalf("save: %d %s", page.status, page.body)
	}
	if code, why := h.tryUpload("big.bin", 2<<20, 0); code != http.StatusUnprocessableEntity || why != "file_too_large" {
		t.Fatalf("2 MB over the 1 MB rule: %d %s", code, why)
	}
	if code, why := h.tryUpload("setup.exe", 10, 0); code != http.StatusUnprocessableEntity || why != "type_not_allowed" {
		t.Fatalf("blocked type: %d %s", code, why)
	}

	// Lifetimes: the default when none is asked for, capped at the longest.
	now := *h.now.Load()
	if link := h.upload("a.txt", 10, 0); !link.ExpiresAt.Equal(now.Add(2 * day)) {
		t.Fatalf("default lifetime: %v", link.ExpiresAt.Sub(now))
	}
	if link := h.upload("b.txt", 10, int64((10 * day).Seconds())); !link.ExpiresAt.Equal(now.Add(3 * day)) {
		t.Fatalf("capped lifetime: %v", link.ExpiresAt.Sub(now))
	}

	// An allow list.
	admin.submit("/admin/rules", url.Values{"allowed": {"pdf, zip"}})
	if code, _ := h.tryUpload("photo.jpg", 10, 0); code != http.StatusUnprocessableEntity {
		t.Fatalf("type outside the allow list: %d", code)
	}
	if code, _ := h.tryUpload("folder.zip", 10, 0); code != http.StatusCreated {
		t.Fatalf("allowed type: %d", code)
	}

	// Back to the server's settings.
	if page := admin.submit("/admin/rules/reset", url.Values{}); !strings.Contains(page.body, "Back to the server") {
		t.Fatalf("reset: %s", page.body)
	}
	if code, _ := h.tryUpload("photo.jpg", 2<<20, 0); code != http.StatusCreated {
		t.Fatalf("after reset: %d", code)
	}
	if page := admin.get("/admin/activity"); !strings.Contains(page.body, "changed the rules") || !strings.Contains(page.body, "reset the rules") {
		t.Fatal("rule changes should be in the activity log")
	}
}

func TestRulesValidation(t *testing.T) {
	h := newHarness(t)
	admin, _ := withAdmin(t, h)
	for _, tc := range []struct {
		form url.Values
		want string
	}{
		{url.Values{"default_days": {"10"}, "max_days": {"5"}}, "can&#39;t be shorter"},
		{url.Values{"max_days": {"0"}}, "whole number from 1 to 365"},
		{url.Values{"default_days": {"two"}}, "whole number"},
		{url.Values{"max_mb": {"999999"}}, "from 1 to 5120"},
		// The server setting (30 days longest) is checked too.
		{url.Values{"default_days": {"40"}}, "can&#39;t be shorter"},
	} {
		page := admin.submit("/admin/rules", tc.form)
		if page.status != http.StatusUnprocessableEntity || !strings.Contains(page.body, tc.want) {
			t.Errorf("%v: %d, want %q in the page", tc.form, page.status, tc.want)
		}
	}
	// What was typed stays in the form after an error.
	page := admin.submit("/admin/rules", url.Values{"default_days": {"10"}, "max_days": {"5"}, "blocked": {".exe"}})
	if !strings.Contains(page.body, `value="10"`) || !strings.Contains(page.body, `value=".exe"`) {
		t.Fatal("the form should keep what was typed")
	}
	if code, _ := h.tryUpload("x.exe", 10, 0); code != http.StatusCreated {
		t.Fatal("nothing should have been saved")
	}
}
