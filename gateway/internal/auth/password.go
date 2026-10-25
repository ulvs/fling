package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters: RFC 9106's second recommended option (64 MiB, 1 pass, 4 lanes).
// Variables so tests can make hashing cheap.
var (
	argonMemoryKiB uint32 = 64 * 1024
	argonTime      uint32 = 1
	argonThreads   uint8  = 4
)

const (
	MinPasswordLength = 12
	maxPasswordBytes  = 1024
)

var ErrBadPasswordHash = errors.New("unrecognised password hash")

// CheckPasswordRules follows NIST SP 800-63B: a minimum length and nothing else forced,
// so long passphrases and password managers work.
func CheckPasswordRules(password, email string) error {
	switch {
	case utf8.RuneCountInString(password) < MinPasswordLength:
		return fmt.Errorf("Use at least %d characters.", MinPasswordLength)
	case len(password) > maxPasswordBytes:
		return errors.New("That password is too long.")
	case strings.EqualFold(strings.TrimSpace(password), strings.TrimSpace(email)):
		return errors.New("Don't use your email address as the password.")
	}
	return nil
}

// HashPassword returns a PHC-format Argon2id hash: $argon2id$v=19$m=…,t=…,p=…$salt$hash.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, 32)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// CheckPassword reports whether password matches encoded, using the parameters stored in it.
func CheckPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrBadPasswordHash
	}
	var version int
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrBadPasswordHash
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, ErrBadPasswordHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrBadPasswordHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, ErrBadPasswordHash
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is checked when an account doesn't exist, so a wrong email takes as long as
// a wrong password and response times don't reveal who has an account.
var dummyHash = func() string {
	h, _ := HashPassword("not-a-real-password")
	return h
}()

// CheckPasswordOrDummy checks against encoded, or against a dummy hash when encoded is empty.
func CheckPasswordOrDummy(password, encoded string) bool {
	if encoded == "" {
		CheckPassword(password, dummyHash)
		return false
	}
	ok, _ := CheckPassword(password, encoded)
	return ok
}

// UseCheapHashingForTests makes Argon2id fast. Only for tests.
func UseCheapHashingForTests() { argonMemoryKiB, argonTime, argonThreads = 1024, 1, 1 }
