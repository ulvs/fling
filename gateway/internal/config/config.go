// Package config reads the gateway's settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type S3 struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	PathStyle bool
}

// Policy holds the built-in default rules until per-group policies exist.
type Policy struct {
	DefaultTTL     time.Duration
	MaxTTL         time.Duration
	MaxUploadBytes int64
}

// OIDCProvider is one sign-in provider. Google and Microsoft have fixed issuers; Generic
// covers any other OpenID Connect provider (Okta, Keycloak, a local mock).
type OIDCProvider struct {
	ClientID     string
	ClientSecret string
	Issuer       string // Generic only
	Name         string // Generic only: the label on the sign-in button
	Tenant       string // Microsoft only: "common" (default), "organizations", "consumers" or a tenant ID
}

func (p OIDCProvider) Enabled() bool { return p.ClientID != "" }

// Members decides who may sign in: an exact email address or any address at a domain.
type Members struct {
	Emails  []string
	Domains []string
}

type Config struct {
	Listen       string
	PublicURL    string // e.g. https://fling.intebitter.se, no trailing slash
	DatabasePath string
	S3           S3
	Policy       Policy

	Google    OIDCProvider
	Microsoft OIDCProvider
	Generic   OIDCProvider
	Members   Members
	// Admins may always sign in and see the admin pages (besides accounts with the admin flag).
	Admins Members

	// LocalAccounts enables accounts the gateway manages itself: invited by an admin,
	// signing in with a passkey or a password (optionally with a two-step code).
	LocalAccounts bool
	// RequireTwoFactor makes password sign-ins also need a code from an authenticator app.
	RequireTwoFactor bool
	// TrustedProxies are reverse proxies whose X-Forwarded-For is believed (for throttling).
	TrustedProxies []netip.Prefix

	Mail Mail

	// DevToken enables a single static bearer token for development.
	DevToken     string
	DevUserEmail string
}

// Mail sends invites and password-reset links through the Resend API. Without an API key the
// admin passes links on by hand.
type Mail struct {
	ResendAPIKey string
	From         string // e.g. "Fling <fling@example.com>", on a domain verified in Resend
}

func (m Mail) Enabled() bool { return m.ResendAPIKey != "" }

// AnyProvider reports whether at least one sign-in provider is configured.
func (c Config) AnyProvider() bool {
	return c.Google.Enabled() || c.Microsoft.Enabled() || c.Generic.Enabled()
}

const minDevTokenLength = 32

// FromEnv builds a Config from FLING_* variables. getenv is os.Getenv in production.
func FromEnv(getenv func(string) string) (Config, error) {
	var errs []error
	get := func(name, fallback string) string {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v
		}
		return fallback
	}
	duration := func(name, fallback string) time.Duration {
		d, err := time.ParseDuration(get(name, fallback))
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("%s: must be a positive duration like 168h", name))
		}
		return d
	}
	boolean := func(name string, fallback bool) bool {
		raw := get(name, strconv.FormatBool(fallback))
		b, err := strconv.ParseBool(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: must be true or false", name))
		}
		return b
	}

	cfg := Config{
		Listen:       get("FLING_LISTEN", ":8080"),
		PublicURL:    strings.TrimRight(get("FLING_PUBLIC_URL", ""), "/"),
		DatabasePath: get("FLING_DATABASE_PATH", "/data/fling-gateway.db"),
		S3: S3{
			Endpoint:  get("FLING_S3_ENDPOINT", ""),
			Region:    get("FLING_S3_REGION", "us-east-1"),
			Bucket:    get("FLING_S3_BUCKET", ""),
			AccessKey: get("FLING_S3_ACCESS_KEY", ""),
			SecretKey: get("FLING_S3_SECRET_KEY", ""),
			PathStyle: boolean("FLING_S3_PATH_STYLE", true),
		},
		Policy: Policy{
			DefaultTTL: duration("FLING_DEFAULT_LINK_TTL", "168h"),
			MaxTTL:     duration("FLING_MAX_LINK_TTL", "720h"),
		},
		Google: OIDCProvider{
			ClientID:     get("FLING_GOOGLE_CLIENT_ID", ""),
			ClientSecret: get("FLING_GOOGLE_CLIENT_SECRET", ""),
		},
		Microsoft: OIDCProvider{
			ClientID:     get("FLING_MICROSOFT_CLIENT_ID", ""),
			ClientSecret: get("FLING_MICROSOFT_CLIENT_SECRET", ""),
			Tenant:       get("FLING_MICROSOFT_TENANT", "common"),
		},
		Generic: OIDCProvider{
			ClientID:     get("FLING_OIDC_CLIENT_ID", ""),
			ClientSecret: get("FLING_OIDC_CLIENT_SECRET", ""),
			Issuer:       strings.TrimRight(get("FLING_OIDC_ISSUER", ""), "/"),
			Name:         get("FLING_OIDC_NAME", "single sign-on"),
		},
		Members: Members{
			Emails:  list(get("FLING_MEMBER_EMAILS", "")),
			Domains: list(get("FLING_MEMBER_DOMAINS", "")),
		},
		Admins:           Members{Emails: list(get("FLING_ADMIN_EMAILS", ""))},
		LocalAccounts:    boolean("FLING_LOCAL_ACCOUNTS", false),
		RequireTwoFactor: boolean("FLING_REQUIRE_TWO_FACTOR", false),
		Mail: Mail{
			ResendAPIKey: get("FLING_RESEND_API_KEY", ""),
			From:         get("FLING_MAIL_FROM", ""),
		},
		DevToken:     get("FLING_DEV_TOKEN", ""),
		DevUserEmail: get("FLING_DEV_USER_EMAIL", "dev@localhost"),
	}
	for _, raw := range list(get("FLING_TRUSTED_PROXIES", "")) {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			addr, addrErr := netip.ParseAddr(raw)
			if addrErr != nil {
				errs = append(errs, fmt.Errorf("FLING_TRUSTED_PROXIES: %q is not an IP address or CIDR range", raw))
				continue
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, prefix.Masked())
	}

	maxBytes, err := strconv.ParseInt(get("FLING_MAX_UPLOAD_BYTES", "2147483648"), 10, 64)
	if err != nil || maxBytes <= 0 {
		errs = append(errs, errors.New("FLING_MAX_UPLOAD_BYTES: must be a positive number of bytes"))
	}
	cfg.Policy.MaxUploadBytes = maxBytes

	if cfg.PublicURL == "" {
		errs = append(errs, errors.New("FLING_PUBLIC_URL is required, e.g. https://fling.example.com"))
	} else if u, err := url.Parse(cfg.PublicURL); err != nil || u.Host == "" {
		errs = append(errs, errors.New("FLING_PUBLIC_URL must be an absolute URL"))
	} else if u.Scheme != "https" && !isLocalHost(u.Hostname()) {
		errs = append(errs, errors.New("FLING_PUBLIC_URL must use https (http is only allowed for localhost)"))
	}
	for name, value := range map[string]string{
		"FLING_S3_ENDPOINT": cfg.S3.Endpoint, "FLING_S3_BUCKET": cfg.S3.Bucket,
		"FLING_S3_ACCESS_KEY": cfg.S3.AccessKey, "FLING_S3_SECRET_KEY": cfg.S3.SecretKey,
	} {
		if value == "" {
			errs = append(errs, fmt.Errorf("%s is required", name))
		}
	}
	if cfg.Policy.MaxTTL < cfg.Policy.DefaultTTL {
		errs = append(errs, errors.New("FLING_MAX_LINK_TTL must be at least FLING_DEFAULT_LINK_TTL"))
	}
	if cfg.DevToken != "" && len(cfg.DevToken) < minDevTokenLength {
		errs = append(errs, fmt.Errorf("FLING_DEV_TOKEN must be at least %d characters", minDevTokenLength))
	}
	for prefix, p := range map[string]OIDCProvider{"FLING_GOOGLE": cfg.Google, "FLING_MICROSOFT": cfg.Microsoft, "FLING_OIDC": cfg.Generic} {
		if p.Enabled() && p.ClientSecret == "" {
			errs = append(errs, fmt.Errorf("%s_CLIENT_SECRET is required when %s_CLIENT_ID is set", prefix, prefix))
		}
	}
	if cfg.Generic.Enabled() {
		if u, err := url.Parse(cfg.Generic.Issuer); err != nil || u.Host == "" {
			errs = append(errs, errors.New("FLING_OIDC_ISSUER must be an absolute URL when FLING_OIDC_CLIENT_ID is set"))
		}
	}
	if cfg.Mail.Enabled() && cfg.Mail.From == "" {
		errs = append(errs, errors.New("FLING_MAIL_FROM is required when FLING_RESEND_API_KEY is set, e.g. Fling <fling@example.com>"))
	}
	switch {
	case !cfg.AnyProvider() && !cfg.LocalAccounts && cfg.DevToken == "":
		errs = append(errs, errors.New("configure a way to sign in: FLING_LOCAL_ACCOUNTS=true, a provider (FLING_GOOGLE_*, FLING_MICROSOFT_* or FLING_OIDC_*), or FLING_DEV_TOKEN for development"))
	case cfg.AnyProvider() && !cfg.LocalAccounts && len(cfg.Members.Emails) == 0 && len(cfg.Members.Domains) == 0 && len(cfg.Admins.Emails) == 0:
		errs = append(errs, errors.New("FLING_MEMBER_EMAILS, FLING_MEMBER_DOMAINS or FLING_ADMIN_EMAILS is required: nobody could sign in"))
	}
	return cfg, errors.Join(errs...)
}

// list splits a comma-separated value into lower-case, trimmed, non-empty items.
func list(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.ToLower(strings.TrimSpace(item)); item != "" {
			out = append(out, strings.TrimPrefix(item, "@"))
		}
	}
	return out
}

// Allows reports whether email may sign in. email must already be verified by the provider.
func (m Members) Allows(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return false
	}
	for _, e := range m.Emails {
		if e == email {
			return true
		}
	}
	domain := email[at+1:]
	for _, d := range m.Domains {
		if d == domain {
			return true
		}
	}
	return false
}

func isLocalHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
