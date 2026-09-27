// Package identity implements users, groups, interactive login with sessions, TOTP MFA,
// password policy and the audit trail reader (PRD §21-26, §66).
package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (OWASP 2024 recommendation: m=64 MiB, t=3, p=2 is well above the
// minimum while keeping a login under ~100 ms on a small server).
const (
	argonMemory  = 64 * 1024
	argonTime    = 3
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16
)

// MinPasswordLength is the shortest password accepted.
const MinPasswordLength = 10

// ErrWeakPassword is returned when a new password does not meet the policy.
var ErrWeakPassword = errors.New("password too weak")

// CheckPolicy enforces a minimum length and at least two character classes.
func CheckPolicy(pw, username string) error {
	if len([]rune(pw)) < MinPasswordLength {
		return fmt.Errorf("%w: use at least %d characters", ErrWeakPassword, MinPasswordLength)
	}
	var lower, upper, digit, other bool
	for _, r := range pw {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			other = true
		}
	}
	classes := 0
	for _, b := range []bool{lower, upper, digit, other} {
		if b {
			classes++
		}
	}
	if classes < 2 {
		return fmt.Errorf("%w: mix letters with numbers or symbols", ErrWeakPassword)
	}
	if username != "" && strings.Contains(strings.ToLower(pw), strings.ToLower(username)) {
		return fmt.Errorf("%w: it must not contain the username", ErrWeakPassword)
	}
	return nil
}

// HashPassword returns a PHC-formatted argon2id hash.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks pw against a hash produced by HashPassword. An empty hash never
// matches (users without a password can only use API tokens).
func VerifyPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory uint32
	var iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	if len(want) > 1<<20 {
		return false
	}
	// #nosec G115 -- len(want) is explicitly bounded to 1 MiB above.
	keyLen := uint32(len(want))
	got := argon2.IDKey([]byte(pw), salt, iterations, memory, threads, keyLen)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is verified against when the user does not exist, so a login attempt takes
// the same time whether or not the username is valid.
var dummyHash, _ = HashPassword("openvms-timing-equalizer")
