package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResendSend(t *testing.T) {
	var got resendRequest
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()

	r := NewResend("re_key", "Fling <fling@example.com>")
	r.Endpoint = srv.URL
	err := r.Send(context.Background(), Message{To: "ann@example.com", Subject: "Hi", Text: "Hello", HTML: "<p>Hello</p>"})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer re_key" || got.From != "Fling <fling@example.com>" || len(got.To) != 1 || got.To[0] != "ann@example.com" || got.Text != "Hello" {
		t.Fatalf("unexpected request %q %+v", auth, got)
	}
}

func TestResendError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"The example.com domain is not verified."}`))
	}))
	defer srv.Close()
	r := NewResend("re_key", "Fling <fling@example.com>")
	r.Endpoint = srv.URL
	err := r.Send(context.Background(), Message{To: "a@b.c", Subject: "s", Text: "t"})
	if err == nil || !strings.Contains(err.Error(), "not verified") || strings.Contains(err.Error(), "re_key") {
		t.Fatalf("want a useful error without the key, got %v", err)
	}
}
