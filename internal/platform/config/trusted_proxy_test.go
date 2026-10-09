package config

import (
	"net/netip"
	"testing"
)

func TestParseCredentialTrustedProxyCIDRs(t *testing.T) {
	got, err := parseCredentialTrustedProxyCIDRs("10.12.0.0/24, 2001:db8::1/128")
	if err != nil || len(got) != 2 || got[0] != netip.MustParsePrefix("10.12.0.0/24") || got[1] != netip.MustParsePrefix("2001:db8::1/128") {
		t.Fatalf("parsed CIDRs = %#v, %v", got, err)
	}
	for _, raw := range []string{"10.0.0.1", "10.0.0.0/33", "10.0.0.0/8,,192.0.2.0/24", "not-a-cidr"} {
		if _, err := parseCredentialTrustedProxyCIDRs(raw); err == nil {
			t.Errorf("parseCredentialTrustedProxyCIDRs(%q) succeeded", raw)
		}
	}
}

func TestLoadReadsCredentialTrustedProxyCIDRs(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("CREDENTIAL_TRUSTED_PROXY_CIDRS", "10.21.0.0/16")
	c, err := Load("test")
	if err != nil || len(c.CredentialTrustedProxyCIDRs) != 1 || c.CredentialTrustedProxyCIDRs[0] != netip.MustParsePrefix("10.21.0.0/16") {
		t.Fatalf("loaded credential proxy CIDRs = %#v, %v", c.CredentialTrustedProxyCIDRs, err)
	}
}

func TestLoadFailsClosedOnInvalidCredentialTrustedProxyCIDRs(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("CREDENTIAL_TRUSTED_PROXY_CIDRS", "10.0.0.0/8,invalid")
	if _, err := Load("test"); err == nil {
		t.Fatal("Load accepted malformed credential proxy CIDRs")
	}
}
