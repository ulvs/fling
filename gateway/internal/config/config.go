// Package config reads the gateway's settings from environment variables.
package config

import (
	"errors"
	"fmt"
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

type Config struct {
	Listen       string
	PublicURL    string // e.g. https://fling.intebitter.se, no trailing slash
	DatabasePath string
	S3           S3
	Policy       Policy

	// DevToken enables a single static bearer token for development until sign-in exists.
	DevToken     string
	DevUserEmail string
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
		DevToken:     get("FLING_DEV_TOKEN", ""),
		DevUserEmail: get("FLING_DEV_USER_EMAIL", "dev@localhost"),
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
	return cfg, errors.Join(errs...)
}

func isLocalHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
