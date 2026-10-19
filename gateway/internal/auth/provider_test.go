package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"golang.org/x/oauth2"

	"github.com/ulvs/fling/gateway/internal/config"
	"github.com/ulvs/fling/gateway/internal/oidctest"
)

func genericProvider(t *testing.T, idp *oidctest.Server) *Provider {
	t.Helper()
	ps := Providers(config.Config{
		PublicURL: "http://localhost:8080",
		Generic:   config.OIDCProvider{ClientID: idp.ClientID, ClientSecret: idp.ClientSecret, Issuer: idp.URL, Name: "Test"},
	}, idp.Client())
	if len(ps) != 1 || ps[0].ID != "oidc" {
		t.Fatalf("want one generic provider, got %v", ps)
	}
	return ps[0]
}

// signIn runs the authorize step and returns the code the provider hands back.
func signIn(t *testing.T, p *Provider, idp *oidctest.Server, state, nonce, verifier string) string {
	t.Helper()
	authURL, err := p.AuthURL(state, nonce, verifier)
	if err != nil {
		t.Fatal(err)
	}
	client := idp.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	back, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || back.Query().Get("state") != state {
		t.Fatalf("bad redirect %q", resp.Header.Get("Location"))
	}
	if got := back.Scheme + "://" + back.Host + back.Path; got != "http://localhost:8080/auth/callback/oidc" {
		t.Fatalf("redirect to %q", got)
	}
	return back.Query().Get("code")
}

func TestExchangeReturnsVerifiedEmail(t *testing.T) {
	idp := oidctest.New(t)
	idp.SignInAs("Ann@Example.com", true)
	p := genericProvider(t, idp)
	verifier := oauth2.GenerateVerifier()
	code := signIn(t, p, idp, "state1", "nonce1", verifier)

	id, err := p.Exchange(context.Background(), code, verifier, "nonce1")
	if err != nil {
		t.Fatal(err)
	}
	if id.Email != "ann@example.com" || id.Provider != "oidc" || id.Subject == "" {
		t.Fatalf("unexpected identity %+v", id)
	}
}

func TestExchangeRejectsUnverifiedEmail(t *testing.T) {
	idp := oidctest.New(t)
	idp.SignInAs("ann@example.com", false)
	p := genericProvider(t, idp)
	verifier := oauth2.GenerateVerifier()
	code := signIn(t, p, idp, "s", "n", verifier)
	if _, err := p.Exchange(context.Background(), code, verifier, "n"); !errors.Is(err, ErrNoVerifiedEmail) {
		t.Fatalf("want ErrNoVerifiedEmail, got %v", err)
	}
}

func TestExchangeRejectsWrongNonceAndVerifier(t *testing.T) {
	idp := oidctest.New(t)
	p := genericProvider(t, idp)
	verifier := oauth2.GenerateVerifier()

	code := signIn(t, p, idp, "s", "right-nonce", verifier)
	if _, err := p.Exchange(context.Background(), code, verifier, "other-nonce"); err == nil {
		t.Fatal("wrong nonce accepted")
	}
	code = signIn(t, p, idp, "s", "n", verifier)
	if _, err := p.Exchange(context.Background(), code, oauth2.GenerateVerifier(), "n"); err == nil {
		t.Fatal("wrong PKCE verifier accepted")
	}
}

func TestMicrosoftEmail(t *testing.T) {
	for name, tc := range map[string]struct {
		c    claims
		want bool
	}{
		"personal account":                {claims{Email: "a@outlook.com", TenantID: msaTenant}, true},
		"work account, domain verified":   {claims{Email: "a@contoso.com", TenantID: "t1", EmailDomainOV: true}, true},
		"work account, verified as text":  {claims{Email: "a@contoso.com", TenantID: "t1", EmailDomainOV: "true"}, true},
		"work account, domain unverified": {claims{Email: "ceo@contoso.com", TenantID: "evil", EmailDomainOV: false}, false},
		"work account, no xms_edov":       {claims{Email: "ceo@contoso.com", TenantID: "evil"}, false},
		"no email":                        {claims{TenantID: msaTenant}, false},
	} {
		_, err := microsoftEmail(tc.c)
		if (err == nil) != tc.want {
			t.Errorf("%s: got err %v, want ok=%v", name, err, tc.want)
		}
	}
}

func TestMicrosoftIssuer(t *testing.T) {
	ok := claims{TenantID: "t1", Issuer: "https://login.microsoftonline.com/t1/v2.0"}
	if err := microsoftIssuer(ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []claims{
		{TenantID: "t1", Issuer: "https://login.microsoftonline.com/t2/v2.0"},
		{TenantID: "", Issuer: "https://login.microsoftonline.com//v2.0"},
		{TenantID: "t1", Issuer: "https://evil.example/t1/v2.0"},
	} {
		if microsoftIssuer(bad) == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestMicrosoftTenantModes(t *testing.T) {
	cfg := config.Config{PublicURL: "https://fling.example.com"}
	for tenant, want := range map[string]struct {
		issuer string
		skip   bool
	}{
		"common":        {"https://login.microsoftonline.com/{tenantid}/v2.0", true},
		"organizations": {"https://login.microsoftonline.com/{tenantid}/v2.0", true},
		"consumers":     {"https://login.microsoftonline.com/" + msaTenant + "/v2.0", false},
		"my-tenant-id":  {"", false},
	} {
		cfg.Microsoft = config.OIDCProvider{ClientID: "id", ClientSecret: "s", Tenant: tenant}
		p := Providers(cfg, nil)[0]
		if p.expectedIssuer != want.issuer || p.skipIssuer != want.skip || (want.skip && p.checkToken == nil) {
			t.Errorf("%s: issuer %q skip %v", tenant, p.expectedIssuer, p.skipIssuer)
		}
		if p.discoveryURL != "https://login.microsoftonline.com/"+tenant+"/v2.0" {
			t.Errorf("%s: discovery %q", tenant, p.discoveryURL)
		}
	}
}
