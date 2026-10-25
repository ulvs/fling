package auth

import (
	"strings"
	"testing"
	"time"
)

func init() {
	// Keep password hashing fast in tests.
	argonMemoryKiB, argonTime, argonThreads = 1024, 1, 1
}

func TestPasswordHashRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=1024,t=1,p=1$") {
		t.Fatalf("unexpected format %q", h)
	}
	if ok, err := CheckPassword("correct horse battery staple", h); !ok || err != nil {
		t.Fatalf("right password rejected: %v", err)
	}
	if ok, _ := CheckPassword("correct horse battery stapl", h); ok {
		t.Fatal("wrong password accepted")
	}
	other, _ := HashPassword("correct horse battery staple")
	if other == h {
		t.Fatal("hashes must be salted")
	}
	if _, err := CheckPassword("x", "$2a$10$bcrypt"); err == nil {
		t.Fatal("foreign hash format accepted")
	}
	if CheckPasswordOrDummy("anything", "") {
		t.Fatal("missing account must never match")
	}
}

func TestPasswordRules(t *testing.T) {
	for pw, ok := range map[string]bool{
		"short":                      false,
		"exactly12chr":               true,
		"åäöåäöåäöåäö":               true, // 12 runes
		"ann@example.com":            false,
		strings.Repeat("x", 2000):    false,
		"a long and easy passphrase": true,
	} {
		if err := CheckPasswordRules(pw, "ann@example.com"); (err == nil) != ok {
			t.Errorf("%q: got %v", pw, err)
		}
	}
}

// RFC 6238 appendix B, SHA-1 secret "12345678901234567890", last 6 of the 8 digits.
func TestTOTPVectors(t *testing.T) {
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"} {
		got, err := totpCode(secret, TOTPStep(time.Unix(unix, 0)))
		if err != nil || got != want {
			t.Errorf("t=%d: got %q, want %q (%v)", unix, got, want, err)
		}
	}
}

func TestCheckTOTPWindowAndReplay(t *testing.T) {
	secret := NewTOTPSecret()
	now := time.Unix(1_800_000_000, 0)
	code, _ := totpCode(secret, TOTPStep(now))
	step, ok := CheckTOTP(secret, code, now, 0)
	if !ok || step != TOTPStep(now) {
		t.Fatal("current code rejected")
	}
	if _, ok := CheckTOTP(secret, code, now, step); ok {
		t.Fatal("replayed code accepted")
	}
	prev, _ := totpCode(secret, TOTPStep(now)-1)
	if _, ok := CheckTOTP(secret, prev[:3]+" "+prev[3:], now, 0); !ok {
		t.Fatal("previous step (clock drift, with a space) rejected")
	}
	old, _ := totpCode(secret, TOTPStep(now)-3)
	if _, ok := CheckTOTP(secret, old, now, 0); ok {
		t.Fatal("code from 90 s ago accepted")
	}
	if !strings.HasPrefix(TOTPURI("Fling Gateway", "ann@example.com", secret), "otpauth://totp/Fling%20Gateway:ann@example.com?") {
		t.Fatal("bad otpauth URI")
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes := NewRecoveryCodes(10)
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 14 || strings.Count(c, "-") != 2 || seen[c] {
			t.Fatalf("bad or duplicate code %q", c)
		}
		seen[c] = true
	}
	if HashRecoveryCode(codes[0]) != HashRecoveryCode(" "+strings.ToUpper(strings.ReplaceAll(codes[0], "-", ""))+" ") {
		t.Fatal("recovery codes should ignore case, spaces and dashes")
	}
}

func TestThrottle(t *testing.T) {
	th := NewThrottle(3, time.Minute)
	now := time.Unix(1000, 0)
	for i := 0; i < 3; i++ {
		if !th.Allowed("k", now) {
			t.Fatalf("blocked after %d failures", i)
		}
		th.Fail("k", now)
	}
	if th.Allowed("k", now.Add(30*time.Second)) {
		t.Fatal("not blocked after 3 failures")
	}
	if !th.Allowed("other", now) {
		t.Fatal("other keys must not be affected")
	}
	if !th.Allowed("k", now.Add(time.Minute)) {
		t.Fatal("still blocked after the window")
	}
	th.Fail("k", now)
	th.Reset("k")
	if !th.Allowed("k", now) {
		t.Fatal("reset didn't clear")
	}
}
