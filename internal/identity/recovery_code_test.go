package identity

import (
	"regexp"
	"testing"
)

func TestNewRecoveryCodeIsSixRandomDigits(t *testing.T) {
	digits := regexp.MustCompile(`^[0-9]{6}$`)
	seen := map[string]bool{}
	for range 200 {
		code, err := newRecoveryCode()
		if err != nil {
			t.Fatal(err)
		}
		if !digits.MatchString(code) {
			t.Fatalf("code %q is not 6 digits", code)
		}
		seen[code] = true
	}
	// 200 draws from 10^6 values: a handful of collisions at most, never a constant.
	if len(seen) < 190 {
		t.Fatalf("only %d distinct codes in 200 draws", len(seen))
	}
}

func TestRecoveryCodeHashMatchesOnlyItsCode(t *testing.T) {
	salt, hash, err := hashRecoveryCode("482913")
	if err != nil {
		t.Fatal(err)
	}
	if len(salt) != 16 || len(hash) != 32 {
		t.Fatalf("salt %d bytes, hash %d bytes; want 16 and 32", len(salt), len(hash))
	}
	if !recoveryCodeMatches("482913", salt, hash) {
		t.Fatal("the code must match its own hash")
	}
	for _, wrong := range []string{"482914", "", "4829130", " 482913"} {
		if recoveryCodeMatches(wrong, salt, hash) {
			t.Fatalf("%q must not match", wrong)
		}
	}
	otherSalt, otherHash, _ := hashRecoveryCode("482913")
	if string(otherSalt) == string(salt) || string(otherHash) == string(hash) {
		t.Fatal("each hash must use a fresh salt")
	}
}
