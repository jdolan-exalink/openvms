package identity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP uses HMAC-SHA1; authenticator apps expect it
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP per RFC 6238: 30-second steps, 6 digits, HMAC-SHA1, compatible with Google
// Authenticator, Authy, 1Password, Aegis, etc.
const (
	totpStep   = 30
	totpDigits = 6
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random 160-bit secret in base32.
func NewTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b32.EncodeToString(b), nil
}

// TOTPURL is the otpauth:// URL authenticator apps import (usually as a QR code).
func TOTPURL(issuer, account, secret string) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", fmt.Sprint(totpDigits))
	v.Set("period", fmt.Sprint(totpStep))
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + v.Encode()
}

func totpAt(secret string, counter uint64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, code%1_000_000), nil
}

// TOTPCode returns the code for time t (used by tests and tools).
func TOTPCode(secret string, t time.Time) (string, error) {
	unix := t.Unix()
	if unix < 0 {
		return "", fmt.Errorf("TOTP time must not be negative")
	}
	return totpAt(secret, uint64(unix/totpStep))
}

// VerifyTOTP accepts the code for t and one step either side (clock drift).
func VerifyTOTP(secret, code string, t time.Time) bool {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return false
	}
	unix := t.Unix()
	if unix < 0 {
		return false
	}
	now := uint64(unix / totpStep)
	ok := false
	counters := []uint64{now, now + 1}
	if now > 0 {
		counters = append([]uint64{now - 1}, counters...)
	}
	for _, c := range counters {
		want, err := totpAt(secret, c)
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			ok = true
		}
	}
	return ok
}
