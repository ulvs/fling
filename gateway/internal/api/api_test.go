package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ulvs/fling/gateway/internal/policy"
	"github.com/ulvs/fling/gateway/internal/storage"
	"github.com/ulvs/fling/gateway/internal/store"
)

const testToken = "test-token-0123456789-0123456789-abcdef"

type fakeObjects struct {
	sizes   map[string]int64 // key -> size, set by the test to simulate an upload
	deleted []string
	deny    bool // Delete answers access denied, like a key without s3:DeleteObject
}

func (f *fakeObjects) Delete(_ context.Context, key string) error {
	if f.deny {
		return storage.ErrAccessDenied
	}
	delete(f.sizes, key)
	f.deleted = append(f.deleted, key)
	return nil
}

func (f *fakeObjects) PresignPut(_ context.Context, key, contentType string, size int64, _ time.Duration) (string, http.Header, error) {
	h := http.Header{}
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", fmt.Sprint(size))
	return "https://s3.test/put/" + key + "?sig=put", h, nil
}

func (f *fakeObjects) PresignGet(_ context.Context, key, name string, _ time.Duration) (string, error) {
	return "https://s3.test/get/" + key + "?sig=get&name=" + name, nil
}

func (f *fakeObjects) Size(_ context.Context, key string) (int64, error) {
	if size, ok := f.sizes[key]; ok {
		return size, nil
	}
	return 0, storage.ErrNotFound
}

func (f *fakeObjects) Ping(context.Context) error { return nil }

type harness struct {
	t       *testing.T
	server  *Server
	handler http.Handler
	objects *fakeObjects
	now     atomic.Pointer[time.Time]
	logs    *bytes.Buffer
}

var dbCounter atomic.Int64

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, fmt.Sprintf("memdb%d?mode=memory&cache=shared", dbCounter.Add(1)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	h := &harness{t: t, objects: &fakeObjects{sizes: map[string]int64{}}, logs: &bytes.Buffer{}}
	start := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	h.now.Store(&start)

	if err := db.EnsureOrg(ctx, "org1", "Test org", start); err != nil {
		t.Fatal(err)
	}
	user, err := db.EnsureUser(ctx, "org1", "Dev@Example.com", start)
	if err != nil {
		t.Fatal(err)
	}
	h.server = &Server{
		Store:     db,
		Objects:   h.objects,
		Policy:    policy.Policy{DefaultTTL: 7 * 24 * time.Hour, MaxTTL: 30 * 24 * time.Hour, MaxUploadBytes: 1000},
		PublicURL: "https://fling.test",
		Log:       slog.New(slog.NewTextHandler(h.logs, nil)),
		Now:       func() time.Time { return *h.now.Load() },
		DevToken:  testToken,
		DevUser:   user,
	}
	h.handler = h.server.Handler()
	return h
}

func (h *harness) advance(d time.Duration) {
	next := h.now.Load().Add(d)
	h.now.Store(&next)
}

func (h *harness) do(method, path string, body any, token string) *httptest.ResponseRecorder {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	return decode[map[string]apiError](t, rec)["error"].Code
}

// upload runs create + simulated PUT + complete and returns the link.
func (h *harness) upload(name string, size int64, ttlSeconds int64) linkResponse {
	h.t.Helper()
	rec := h.do("POST", "/api/v1/uploads", map[string]any{"name": name, "size": size, "contentType": "text/plain", "ttlSeconds": ttlSeconds}, testToken)
	if rec.Code != http.StatusCreated {
		h.t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	created := decode[createUploadResponse](h.t, rec)
	key := strings.SplitN(strings.TrimPrefix(created.Upload.URL, "https://s3.test/put/"), "?", 2)[0]
	h.objects.sizes[key] = size

	rec = h.do("POST", "/api/v1/uploads/"+created.UploadID+"/complete", nil, testToken)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("complete: %d %s", rec.Code, rec.Body)
	}
	return decode[linkResponse](h.t, rec)
}

func slugOf(link linkResponse) string { return strings.TrimPrefix(link.URL, "https://fling.test/l/") }

func TestUploadAndDownload(t *testing.T) {
	h := newHarness(t)

	rec := h.do("POST", "/api/v1/uploads", map[string]any{"name": `C:\Users\x\räksmörgås.txt`, "size": 42, "contentType": "text/plain"}, testToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	created := decode[createUploadResponse](t, rec)
	if created.Upload.Method != "PUT" || created.Upload.Headers["Content-Length"] != "42" {
		t.Fatalf("unexpected upload target %+v", created.Upload)
	}
	if !strings.HasSuffix(strings.SplitN(created.Upload.URL, "?", 2)[0], "/räksmörgås.txt") || strings.Contains(created.Upload.URL, "Users") {
		t.Fatalf("object key should use only the base name: %s", created.Upload.URL)
	}

	// Completing before the object exists is refused.
	rec = h.do("POST", "/api/v1/uploads/"+created.UploadID+"/complete", nil, testToken)
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "not_uploaded" {
		t.Fatalf("want not_uploaded, got %d %s", rec.Code, rec.Body)
	}

	key := strings.SplitN(strings.TrimPrefix(created.Upload.URL, "https://s3.test/put/"), "?", 2)[0]
	h.objects.sizes[key] = 42
	rec = h.do("POST", "/api/v1/uploads/"+created.UploadID+"/complete", nil, testToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body)
	}
	link := decode[linkResponse](t, rec)
	if !strings.HasPrefix(link.URL, "https://fling.test/l/") || len(slugOf(link)) != 22 {
		t.Fatalf("unexpected link %s", link.URL)
	}
	if want := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC); !link.ExpiresAt.Equal(want) {
		t.Fatalf("default TTL: got %v want %v", link.ExpiresAt, want)
	}

	// Completing twice is refused.
	if rec = h.do("POST", "/api/v1/uploads/"+created.UploadID+"/complete", nil, testToken); rec.Code != http.StatusConflict {
		t.Fatalf("second complete: %d", rec.Code)
	}

	// The public link redirects to a fresh presigned GET.
	rec = h.do("GET", "/l/"+slugOf(link), nil, "")
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "https://s3.test/get/") {
		t.Fatalf("download: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("missing security headers: %v", rec.Header())
	}

	list := decode[map[string][]linkResponse](t, h.do("GET", "/api/v1/links", nil, testToken))["links"]
	if len(list) != 1 || list[0].Downloads != 1 || list[0].Name != "räksmörgås.txt" || list[0].Size != 42 {
		t.Fatalf("unexpected list %+v", list)
	}
}

func TestRulesAreEnforced(t *testing.T) {
	h := newHarness(t)
	rec := h.do("POST", "/api/v1/uploads", map[string]any{"name": "big.bin", "size": 1001}, testToken)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(t, rec) != "file_too_large" {
		t.Fatalf("want file_too_large, got %d %s", rec.Code, rec.Body)
	}
	rec = h.do("POST", "/api/v1/uploads", map[string]any{"name": "../", "size": 10}, testToken)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(t, rec) != "bad_name" {
		t.Fatalf("want bad_name, got %d %s", rec.Code, rec.Body)
	}
	rec = h.do("POST", "/api/v1/uploads", map[string]any{"name": "x.txt", "size": 10, "surprise": true}, testToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown fields should be rejected, got %d", rec.Code)
	}

	// A requested lifetime above the maximum is capped.
	link := h.upload("long.txt", 10, int64((365 * 24 * time.Hour).Seconds()))
	if want := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC); !link.ExpiresAt.Equal(want) {
		t.Fatalf("TTL not capped: %v", link.ExpiresAt)
	}
}

func TestAuthentication(t *testing.T) {
	h := newHarness(t)
	for _, token := range []string{"", "wrong-token-wrong-token-wrong-token-xx"} {
		rec := h.do("GET", "/api/v1/links", nil, token)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("token %q: got %d", token, rec.Code)
		}
	}
	h.server.DevToken = ""
	if rec := h.do("GET", "/api/v1/links", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("empty dev token must not authenticate an empty bearer: %d", rec.Code)
	}
}

func TestRevokeAndExpiry(t *testing.T) {
	h := newHarness(t)
	revoked := h.upload("a.txt", 5, 0)
	short := h.upload("b.txt", 5, 3600)

	if rec := h.do("DELETE", "/api/v1/links/"+revoked.ID, nil, testToken); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d", rec.Code)
	}
	if rec := h.do("GET", "/l/"+slugOf(revoked), nil, ""); rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "turned off") {
		t.Fatalf("revoked link: %d", rec.Code)
	}
	if rec := h.do("DELETE", "/api/v1/links/nope", nil, testToken); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown link revoke: %d", rec.Code)
	}

	if rec := h.do("GET", "/l/"+slugOf(short), nil, ""); rec.Code != http.StatusFound {
		t.Fatalf("fresh link: %d", rec.Code)
	}
	h.advance(time.Hour)
	if rec := h.do("GET", "/l/"+slugOf(short), nil, ""); rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "expired") {
		t.Fatalf("expired link: %d", rec.Code)
	}
	if rec := h.do("GET", "/l/does-not-exist", nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown slug: %d", rec.Code)
	}
}

func TestUsersOnlySeeTheirOwnUploads(t *testing.T) {
	h := newHarness(t)
	link := h.upload("mine.txt", 5, 0)

	other, err := h.server.Store.EnsureUser(context.Background(), "org1", "other@example.com", *h.now.Load())
	if err != nil {
		t.Fatal(err)
	}
	h.server.DevUser = other
	if list := decode[map[string][]linkResponse](t, h.do("GET", "/api/v1/links", nil, testToken))["links"]; len(list) != 0 {
		t.Fatalf("other user sees %d links", len(list))
	}
	if rec := h.do("DELETE", "/api/v1/links/"+link.ID, nil, testToken); rec.Code != http.StatusNotFound {
		t.Fatalf("other user revoked a link: %d", rec.Code)
	}
}

func TestLogsNeverContainSlugsOrSignedURLs(t *testing.T) {
	h := newHarness(t)
	link := h.upload("secret.txt", 5, 0)
	h.do("GET", "/l/"+slugOf(link), nil, "")
	logs := h.logs.String()
	if strings.Contains(logs, slugOf(link)) || strings.Contains(logs, "sig=") || strings.Contains(logs, testToken) {
		t.Fatalf("logs leak secrets:\n%s", logs)
	}
	if !strings.Contains(logs, "route=\"GET /l/{slug}\"") {
		t.Fatalf("expected route pattern in logs:\n%s", logs)
	}
}

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":             "report.pdf",
		`C:\dir\report.pdf`:      "report.pdf",
		"/etc/passwd":            "passwd",
		"  spaced name.txt  ":    "spaced name.txt",
		"tab\tname.txt":          "tabname.txt",
		"..":                     "",
		"":                       "",
		strings.Repeat("a", 256): "",
	}
	for in, want := range cases {
		got, ok := cleanName(in)
		if (want == "" && ok) || (want != "" && (!ok || got != want)) {
			t.Errorf("cleanName(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}
