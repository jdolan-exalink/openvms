package provision

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestAgentDialAddressPolicy(t *testing.T) {
	for _, tc := range []struct {
		ip, scheme string
		ok         bool
	}{
		// Never: this host, link-local (cloud metadata), multicast, unspecified.
		{"127.0.0.1", "https", false}, {"127.0.0.1", "http", false}, {"::1", "https", false},
		{"169.254.169.254", "https", false}, {"169.254.1.1", "http", false}, {"fe80::1", "https", false},
		{"0.0.0.0", "https", false}, {"::", "https", false}, {"224.0.0.1", "https", false}, {"255.255.255.255", "https", false},
		{"::ffff:127.0.0.1", "https", false}, {"::ffff:169.254.169.254", "https", false},
		// A LAN, a VPN (including Tailscale's 100.64/10) or a private IPv6 range: either scheme.
		{"10.0.15.2", "http", true}, {"172.16.0.9", "http", true}, {"192.168.1.20", "http", true},
		{"100.64.3.7", "http", true}, {"100.127.255.254", "http", true}, {"fd12:3456::1", "http", true},
		{"10.0.15.2", "https", true},
		// A public address: HTTPS only, so the bearer token never crosses the internet in clear.
		{"203.0.113.9", "https", true}, {"203.0.113.9", "http", false}, {"2001:db8::1", "https", true}, {"2001:db8::1", "http", false},
		{"100.128.0.1", "http", false},
	} {
		err := checkAgentDialAddress(net.ParseIP(tc.ip), tc.scheme)
		if (err == nil) != tc.ok {
			t.Errorf("%s over %s: err = %v, want ok=%v", tc.ip, tc.scheme, err, tc.ok)
		}
	}
}

func TestGuardedDialResolvesOnceChecksEveryAddressAndDialsAnApprovedOne(t *testing.T) {
	var dialed string
	dial := func(_ context.Context, _, addr string) (net.Conn, error) {
		dialed = addr
		return nil, errors.New("stop")
	}
	lookup := func(_ context.Context, host string) ([]net.IP, error) {
		switch host {
		case "agent.example.com":
			return []net.IP{net.ParseIP("203.0.113.9")}, nil
		case "mixed.example.com": // one good answer next to a forbidden one: DNS rebinding material
			return []net.IP{net.ParseIP("203.0.113.9"), net.ParseIP("169.254.169.254")}, nil
		case "empty.example.com":
			return nil, nil
		}
		return nil, errors.New("no such host")
	}
	guarded := guardedDialContext("https", lookup, dial)

	_, _ = guarded(context.Background(), "tcp", "agent.example.com:443")
	if dialed != "203.0.113.9:443" {
		t.Fatalf("dialed %q, want the resolved and approved address, not the name", dialed)
	}
	for _, addr := range []string{"mixed.example.com:443", "empty.example.com:443", "unknown.example.com:443", "127.0.0.1:443", "169.254.169.254:80"} {
		dialed = ""
		if _, err := guarded(context.Background(), "tcp", addr); err == nil || dialed != "" {
			t.Errorf("%s: err = %v dialed = %q; want a refusal before any connection", addr, err, dialed)
		}
	}
}
