package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Two-step codes from an authenticator app: TOTP (RFC 6238) with the defaults every app
// supports: SHA-1, 6 digits, 30-second steps.
const (
	totpPeriod = 30
	totpDigits = 6
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random 160-bit secret in base32, as authenticator apps expect.
func NewTOTPSecret() string {
	b := make([]byte, 20)
	rand.Read(b)
	return b32.EncodeToString(b)
}

// TOTPURI is what the QR code holds: otpauth://totp/<issuer>:<account>?secret=…&issuer=….
func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"},
		"digits": {fmt.Sprint(totpDigits)}, "period": {fmt.Sprint(totpPeriod)}}
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// TOTPStep is the 30-second time step t falls in.
func TOTPStep(t time.Time) int64 { return t.Unix() / totpPeriod }

func totpCode(secret string, step int64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, value%1_000_000), nil
}

// CheckTOTP accepts a code from the current step or one step either side (clock drift), but
// only from a step later than lastStep, so an observed code can't be replayed. It returns the
// step that matched; store it as the new lastStep.
func CheckTOTP(secret, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	current := TOTPStep(now)
	for step := current - 1; step <= current+1; step++ {
		if step <= lastStep {
			continue
		}
		want, err := totpCode(secret, step)
		if err == nil && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// NewRecoveryCodes returns n one-time codes like "k7m2-q9xd-4tfa" (60 bits each) for when the
// authenticator app is lost. Store only their hashes (HashRecoveryCode).
func NewRecoveryCodes(n int) []string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789" // no 0/o, 1/l/i
	codes := make([]string, n)
	for i := range codes {
		b := make([]byte, 12)
		rand.Read(b)
		var sb strings.Builder
		for j, c := range b {
			if j > 0 && j%4 == 0 {
				sb.WriteByte('-')
			}
			sb.WriteByte(alphabet[int(c)%len(alphabet)])
		}
		codes[i] = sb.String()
	}
	return codes
}

// HashRecoveryCode normalises (case, spaces, dashes) and hashes a recovery code. The codes
// are random, so a plain SHA-256 is enough.
func HashRecoveryCode(code string) string {
	code = strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// TOTPCode is the code an authenticator app shows for secret at time t.
func TOTPCode(secret string, t time.Time) (string, error) { return totpCode(secret, TOTPStep(t)) }
