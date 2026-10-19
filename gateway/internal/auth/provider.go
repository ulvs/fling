// Package auth signs users in with OpenID Connect providers (Google, Microsoft, or any
// other OIDC issuer) and turns the result into a verified email address.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/ulvs/fling/gateway/internal/config"
)

// ErrNoVerifiedEmail means the provider vouched for the account but not for its email
// address, which is what the member list is matched against.
var ErrNoVerifiedEmail = errors.New("the account has no verified email address")

const (
	googleIssuer = "https://accounts.google.com"
	msLogin      = "https://login.microsoftonline.com/"
	// msaTenant is the tenant ID of personal Microsoft accounts (outlook.com, hotmail.com, ...).
	msaTenant = "9188040d-6c67-4c5b-b112-36a304b66dad"
)

// Identity is a signed-in account whose email address the provider has verified.
type Identity struct {
	Provider string
	Subject  string
	Email    string
}

// Provider is one configured identity provider. Discovery happens on first use, so the
// gateway starts even while a provider is unreachable.
type Provider struct {
	ID   string // used in URLs: google, microsoft, oidc
	Name string // shown on the sign-in button

	discoveryURL   string
	expectedIssuer string // overrides the discovery document's issuer check (Microsoft multi-tenant)
	skipIssuer     bool   // the issuer is checked per token by checkToken instead
	clientID       string
	clientSecret   string
	redirectURL    string
	authParams     []oauth2.AuthCodeOption
	client         *http.Client

	checkToken func(c claims) error
	email      func(c claims) (string, error)

	mu       sync.Mutex
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
}

type claims struct {
	Issuer        string `json:"iss"`
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"`
	TenantID      string `json:"tid"`
	EmailDomainOV any    `json:"xms_edov"`
}

// Providers builds the providers enabled in cfg, in the order they appear on the sign-in page.
// client is used for all calls to the providers; nil means a client with a 15 s timeout.
func Providers(cfg config.Config, client *http.Client) []*Provider {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	callback := func(id string) string { return cfg.PublicURL + "/auth/callback/" + id }
	selectAccount := oauth2.SetAuthURLParam("prompt", "select_account")
	var out []*Provider

	if p := cfg.Microsoft; p.Enabled() {
		ms := &Provider{
			ID: "microsoft", Name: "Microsoft", clientID: p.ClientID, clientSecret: p.ClientSecret,
			redirectURL: callback("microsoft"), client: client, authParams: []oauth2.AuthCodeOption{selectAccount},
			discoveryURL: msLogin + p.Tenant + "/v2.0", email: microsoftEmail,
		}
		switch strings.ToLower(p.Tenant) {
		case "common", "organizations":
			// These discovery documents name the issuer as a template; each token's issuer
			// is checked against the tenant it says it comes from.
			ms.expectedIssuer = msLogin + "{tenantid}/v2.0"
			ms.skipIssuer = true
			ms.checkToken = microsoftIssuer
		case "consumers":
			ms.expectedIssuer = msLogin + msaTenant + "/v2.0"
		}
		out = append(out, ms)
	}
	if p := cfg.Google; p.Enabled() {
		out = append(out, &Provider{
			ID: "google", Name: "Google", clientID: p.ClientID, clientSecret: p.ClientSecret,
			redirectURL: callback("google"), client: client, authParams: []oauth2.AuthCodeOption{selectAccount},
			discoveryURL: googleIssuer, email: verifiedEmail,
		})
	}
	if p := cfg.Generic; p.Enabled() {
		out = append(out, &Provider{
			ID: "oidc", Name: p.Name, clientID: p.ClientID, clientSecret: p.ClientSecret,
			redirectURL: callback("oidc"), client: client,
			discoveryURL: p.Issuer, email: verifiedEmail,
		})
	}
	return out
}

func (p *Provider) init() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.verifier != nil {
		return nil
	}
	// This context outlives the request: the verifier refreshes signing keys with it.
	ctx := oidc.ClientContext(context.Background(), p.client)
	if p.expectedIssuer != "" {
		ctx = oidc.InsecureIssuerURLContext(ctx, p.expectedIssuer)
	}
	discovered, err := oidc.NewProvider(ctx, p.discoveryURL)
	if err != nil {
		return fmt.Errorf("%s discovery: %w", p.ID, err)
	}
	p.verifier = discovered.VerifierContext(ctx, &oidc.Config{ClientID: p.clientID, SkipIssuerCheck: p.skipIssuer})
	p.oauth = oauth2.Config{
		ClientID: p.clientID, ClientSecret: p.clientSecret, RedirectURL: p.redirectURL,
		Endpoint: discovered.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "email", "profile"},
	}
	return nil
}

// AuthURL is where the browser goes to sign in. verifier is the PKCE code verifier.
func (p *Provider) AuthURL(state, nonce, verifier string) (string, error) {
	if err := p.init(); err != nil {
		return "", err
	}
	opts := append([]oauth2.AuthCodeOption{oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}, p.authParams...)
	return p.oauth.AuthCodeURL(state, opts...), nil
}

// Exchange redeems the authorization code and returns the verified identity.
func (p *Provider) Exchange(ctx context.Context, code, verifier, nonce string) (Identity, error) {
	if err := p.init(); err != nil {
		return Identity{}, err
	}
	token, err := p.oauth.Exchange(context.WithValue(ctx, oauth2.HTTPClient, p.client), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Identity{}, fmt.Errorf("%s token exchange: %w", p.ID, err)
	}
	raw, _ := token.Extra("id_token").(string)
	if raw == "" {
		return Identity{}, fmt.Errorf("%s returned no ID token", p.ID)
	}
	idToken, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return Identity{}, fmt.Errorf("%s ID token: %w", p.ID, err)
	}
	if idToken.Nonce != nonce {
		return Identity{}, fmt.Errorf("%s ID token: nonce mismatch", p.ID)
	}
	var c claims
	if err := idToken.Claims(&c); err != nil {
		return Identity{}, err
	}
	if p.checkToken != nil {
		if err := p.checkToken(c); err != nil {
			return Identity{}, fmt.Errorf("%s ID token: %w", p.ID, err)
		}
	}
	email, err := p.email(c)
	if err != nil {
		return Identity{}, err
	}
	return Identity{Provider: p.ID, Subject: idToken.Subject, Email: strings.ToLower(strings.TrimSpace(email))}, nil
}

// verifiedEmail is the standard OIDC rule: email counts only with email_verified = true.
func verifiedEmail(c claims) (string, error) {
	if c.Email == "" || !truthy(c.EmailVerified) {
		return "", ErrNoVerifiedEmail
	}
	return c.Email, nil
}

// microsoftEmail accepts the email claim of personal Microsoft accounts, and of work or
// school accounts only when Microsoft says the tenant owns the email domain (xms_edov).
// Without that check any tenant admin could give a user someone else's address.
func microsoftEmail(c claims) (string, error) {
	if c.Email == "" {
		return "", ErrNoVerifiedEmail
	}
	if c.TenantID == msaTenant || truthy(c.EmailDomainOV) {
		return c.Email, nil
	}
	return "", ErrNoVerifiedEmail
}

// microsoftIssuer checks a multi-tenant token's issuer against its own tenant ID.
func microsoftIssuer(c claims) error {
	if c.TenantID == "" || c.Issuer != msLogin+c.TenantID+"/v2.0" {
		return fmt.Errorf("issuer %q doesn't match tenant %q", c.Issuer, c.TenantID)
	}
	return nil
}

// truthy reads boolean claims, which some providers send as strings or numbers.
func truthy(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.EqualFold(b, "true") || b == "1"
	case float64:
		return b == 1
	}
	return false
}
