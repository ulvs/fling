// Package oidctest is a minimal in-process OpenID Connect provider for tests: discovery,
// signing keys, an authorize endpoint that signs in whoever the test says, and a token
// endpoint that checks the client secret and PKCE.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

type Server struct {
	*httptest.Server
	ClientID     string
	ClientSecret string

	key    *rsa.PrivateKey
	signer jose.Signer

	mu     sync.Mutex
	claims map[string]any // who the next sign-in is
	codes  map[string]grant
}

type grant struct {
	redirectURI string
	challenge   string
	nonce       string
	claims      map[string]any
}

// New starts a provider. Close it with t.Cleanup (done automatically).
func New(t *testing.T) *Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{ClientID: "test-client", ClientSecret: "test-secret", key: key, signer: signer, codes: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", s.discovery)
	mux.HandleFunc("GET /jwks", s.jwks)
	mux.HandleFunc("GET /authorize", s.authorize)
	mux.HandleFunc("POST /token", s.token)
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	s.SignInAs("ann@example.com", true)
	return s
}

// SignInAs sets who the next sign-in returns.
func (s *Server) SignInAs(email string, verified bool) {
	s.SetClaims(map[string]any{"sub": "sub-" + email, "email": email, "email_verified": verified})
}

// SetClaims sets the exact extra claims of the next sign-in's ID token.
func (s *Server) SetClaims(c map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claims = maps.Clone(c)
}

func (s *Server) discovery(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                s.URL,
		"authorization_endpoint":                s.URL + "/authorize",
		"token_endpoint":                        s.URL + "/token",
		"jwks_uri":                              s.URL + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

func (s *Server) jwks(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
		{Key: &s.key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"},
	}})
}

// authorize signs in the configured user immediately and redirects back with a code.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != s.ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "bad authorize request", http.StatusBadRequest)
		return
	}
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	code := randomString()
	s.mu.Lock()
	s.codes[code] = grant{redirectURI: q.Get("redirect_uri"), challenge: q.Get("code_challenge"), nonce: q.Get("nonce"), claims: maps.Clone(s.claims)}
	s.mu.Unlock()
	back := redirect.Query()
	back.Set("code", code)
	back.Set("state", q.Get("state"))
	redirect.RawQuery = back.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != s.ClientID || secret != s.ClientSecret {
		oauthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	s.mu.Lock()
	g, found := s.codes[r.PostForm.Get("code")]
	delete(s.codes, r.PostForm.Get("code"))
	s.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !found || r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("redirect_uri") != g.redirectURI ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	now := time.Now()
	body := map[string]any{"iss": s.URL, "aud": s.ClientID, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "nonce": g.nonce}
	maps.Copy(body, g.claims)
	payload, _ := json.Marshal(body)
	signed, err := s.signer.Sign(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	idToken, _ := signed.CompactSerialize()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"access_token": randomString(), "token_type": "Bearer", "expires_in": 300, "id_token": idToken})
}

func oauthError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func randomString() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
