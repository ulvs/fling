package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ulvs/fling/gateway/internal/storage"
)

func keyOf(t *testing.T, h *harness, link linkResponse) string {
	t.Helper()
	l, err := h.server.Store.LinkBySlug(context.Background(), slugOf(link))
	if err != nil {
		t.Fatal(err)
	}
	return l.File.ObjectKey
}

func TestDeleteExpiredFiles(t *testing.T) {
	h := newHarness(t)
	h.server.DeleteFiles, h.server.DeleteAfter = true, 24*time.Hour
	ctx := context.Background()

	short := h.upload("short.txt", 10, 3600)           // expires in an hour
	long := h.upload("long.txt", 10, int64(7*24*3600)) // a week
	revoked := h.upload("revoked.txt", 10, int64(7*24*3600))
	if rec := h.do("DELETE", "/api/v1/links/"+revoked.ID, nil, testToken); rec.Code != http.StatusNoContent {
		t.Fatal("revoke failed")
	}
	// An upload that was announced but never completed.
	if rec := h.do("POST", "/api/v1/uploads", map[string]any{"name": "abandoned.bin", "size": 10}, testToken); rec.Code != http.StatusCreated {
		t.Fatal("create failed")
	}

	if n, err := h.server.DeleteExpiredFiles(ctx); err != nil || n != 0 {
		t.Fatalf("nothing is old enough yet: %d %v", n, err)
	}

	h.advance(time.Hour + 24*time.Hour + time.Minute)
	n, err := h.server.DeleteExpiredFiles(ctx)
	if err != nil || n != 3 {
		t.Fatalf("want short, revoked and abandoned deleted, got %d %v (%v)", n, err, h.objects.deleted)
	}
	for _, l := range []linkResponse{short, revoked} {
		if !slices.Contains(h.objects.deleted, keyOf(t, h, l)) {
			t.Errorf("%s not deleted", l.Name)
		}
	}
	if slices.Contains(h.objects.deleted, keyOf(t, h, long)) {
		t.Error("a file with a live link was deleted")
	}
	if !slices.ContainsFunc(h.objects.deleted, func(k string) bool { return strings.HasSuffix(k, "/abandoned.bin") }) {
		t.Error("the abandoned upload wasn't deleted")
	}

	// Each file is deleted once.
	if n, _ := h.server.DeleteExpiredFiles(ctx); n != 0 {
		t.Fatalf("deleted again: %d", n)
	}
	// The live link still works; the expired one says it expired.
	if rec := h.do("GET", "/l/"+slugOf(long), nil, ""); rec.Code != http.StatusFound {
		t.Fatalf("live link: %d", rec.Code)
	}
	if rec := h.do("GET", "/l/"+slugOf(short), nil, ""); rec.Code != http.StatusGone {
		t.Fatalf("expired link: %d", rec.Code)
	}
}

func TestDeleteExpiredFilesOffOrDenied(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.upload("a.txt", 10, 3600)
	h.advance(48 * time.Hour)

	if n, err := h.server.DeleteExpiredFiles(ctx); n != 0 || err != nil {
		t.Fatalf("clean-up is off by default in the server struct: %d %v", n, err)
	}

	h.server.DeleteFiles, h.server.DeleteAfter = true, 24*time.Hour
	h.objects.deny = true
	if _, err := h.server.DeleteExpiredFiles(ctx); !errors.Is(err, storage.ErrAccessDenied) {
		t.Fatalf("want access denied, got %v", err)
	}
	// Not marked deleted, so it's tried again once the key may delete.
	h.objects.deny = false
	if n, err := h.server.DeleteExpiredFiles(ctx); n != 1 || err != nil {
		t.Fatalf("retry: %d %v", n, err)
	}
}

func TestHumanDelay(t *testing.T) {
	for d, want := range map[time.Duration]string{
		24 * time.Hour: "a day", 72 * time.Hour: "3 days", time.Hour: "an hour", 6 * time.Hour: "6 hours", 30 * time.Minute: "30m0s", 90 * time.Minute: "1h30m0s",
	} {
		if got := humanDelay(d); got != want {
			t.Errorf("%v: %q", d, got)
		}
	}
}
