package config

import (
	"strings"
	"testing"
	"time"
)

func env(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func valid() map[string]string {
	return map[string]string{
		"FLING_PUBLIC_URL":    "https://fling.example.com/",
		"FLING_S3_ENDPOINT":   "https://s3.example.com",
		"FLING_S3_BUCKET":     "fling",
		"FLING_S3_ACCESS_KEY": "key",
		"FLING_S3_SECRET_KEY": "secret",
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := FromEnv(env(valid()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicURL != "https://fling.example.com" {
		t.Errorf("trailing slash not trimmed: %q", cfg.PublicURL)
	}
	if cfg.Policy.DefaultTTL != 168*time.Hour || cfg.Policy.MaxTTL != 720*time.Hour {
		t.Errorf("unexpected TTLs %v %v", cfg.Policy.DefaultTTL, cfg.Policy.MaxTTL)
	}
	if !cfg.S3.PathStyle || cfg.S3.Region != "us-east-1" || cfg.Listen != ":8080" {
		t.Errorf("unexpected defaults %+v", cfg)
	}
}

func TestRejectsPlainHTTPExceptLocalhost(t *testing.T) {
	v := valid()
	v["FLING_PUBLIC_URL"] = "http://fling.example.com"
	if _, err := FromEnv(env(v)); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("want https error, got %v", err)
	}
	v["FLING_PUBLIC_URL"] = "http://localhost:8080"
	if _, err := FromEnv(env(v)); err != nil {
		t.Fatalf("localhost should be allowed: %v", err)
	}
}

func TestReportsAllMissingSettings(t *testing.T) {
	_, err := FromEnv(env(map[string]string{}))
	if err == nil {
		t.Fatal("want error")
	}
	for _, name := range []string{"FLING_PUBLIC_URL", "FLING_S3_ENDPOINT", "FLING_S3_BUCKET", "FLING_S3_ACCESS_KEY", "FLING_S3_SECRET_KEY"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("missing %s in %v", name, err)
		}
	}
}

func TestShortDevTokenRejected(t *testing.T) {
	v := valid()
	v["FLING_DEV_TOKEN"] = "short"
	if _, err := FromEnv(env(v)); err == nil {
		t.Fatal("want error for short dev token")
	}
}

func TestMaxTTLMustCoverDefault(t *testing.T) {
	v := valid()
	v["FLING_DEFAULT_LINK_TTL"] = "72h"
	v["FLING_MAX_LINK_TTL"] = "24h"
	if _, err := FromEnv(env(v)); err == nil {
		t.Fatal("want error")
	}
}
