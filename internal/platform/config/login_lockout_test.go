package config

import "testing"

func TestLoginLockoutDefaultsAndOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("LOGIN_MAX_FAILURES", "")
	t.Setenv("LOGIN_LOCKOUT", "")
	c, err := Load("test")
	if err != nil {
		t.Fatal(err)
	}
	if c.LoginMaxFailures != 5 || c.LoginLockout.String() != "15m0s" {
		t.Fatalf("defaults = %d/%s, want 5/15m", c.LoginMaxFailures, c.LoginLockout)
	}

	t.Setenv("LOGIN_MAX_FAILURES", "0")
	t.Setenv("LOGIN_LOCKOUT", "2h")
	c, err = Load("test")
	if err != nil {
		t.Fatal(err)
	}
	if c.LoginMaxFailures != 0 || c.LoginLockout.String() != "2h0m0s" {
		t.Fatalf("overrides = %d/%s, want 0/2h", c.LoginMaxFailures, c.LoginLockout)
	}
}

func TestLoginLockoutInvalidValuesUseSecureDefaults(t *testing.T) {
	for _, value := range []string{"-1", "invalid", "2147483648"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://x")
			t.Setenv("LOGIN_MAX_FAILURES", value)
			c, err := Load("test")
			if err != nil {
				t.Fatal(err)
			}
			if c.LoginMaxFailures != 5 {
				t.Fatalf("LOGIN_MAX_FAILURES=%q loaded as %d, want secure default 5", value, c.LoginMaxFailures)
			}
		})
	}
	for _, value := range []string{"-15m", "0", "invalid"} {
		t.Run("duration "+value, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://x")
			t.Setenv("LOGIN_LOCKOUT", value)
			c, err := Load("test")
			if err != nil {
				t.Fatal(err)
			}
			if c.LoginLockout.String() != "15m0s" {
				t.Fatalf("LOGIN_LOCKOUT=%q loaded as %s, want secure default 15m", value, c.LoginLockout)
			}
		})
	}
}
