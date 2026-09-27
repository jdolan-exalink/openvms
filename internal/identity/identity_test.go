package identity

import (
	"errors"
	"testing"
	"time"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	h, err := HashPassword("Correct-Horse-9")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(h, "Correct-Horse-9") {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword(h, "Correct-Horse-8") {
		t.Fatal("wrong password accepted")
	}
	if VerifyPassword("", "anything") {
		t.Fatal("empty hash must never match")
	}
	h2, _ := HashPassword("Correct-Horse-9")
	if h == h2 {
		t.Fatal("hashes must be salted")
	}
}

func TestPasswordPolicy(t *testing.T) {
	cases := map[string]bool{
		"short1":              false,
		"alllowercaseletters": false,
		"lowercase-and-1234":  true,
		"juanperez-2026":      false, // contains the username
	}
	for pw, ok := range cases {
		err := CheckPolicy(pw, "juanperez")
		if ok != (err == nil) {
			t.Errorf("%q: got %v", pw, err)
		}
		if err != nil && !errors.Is(err, ErrWeakPassword) {
			t.Errorf("%q: error should wrap ErrWeakPassword", pw)
		}
	}
}

// RFC 6238 appendix B, SHA1, secret "12345678901234567890".
func TestTOTPVectors(t *testing.T) {
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	vectors := map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"}
	for ts, want := range vectors {
		got, err := TOTPCode(secret, time.Unix(ts, 0))
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("t=%d: got %s want %s", ts, got, want)
		}
	}
	now := time.Unix(1_800_000_000, 0)
	code, _ := TOTPCode(secret, now.Add(-30*time.Second))
	if !VerifyTOTP(secret, code, now) {
		t.Error("previous step should be accepted")
	}
	old, _ := TOTPCode(secret, now.Add(-90*time.Second))
	if VerifyTOTP(secret, old, now) {
		t.Error("code from three steps ago accepted")
	}
}
