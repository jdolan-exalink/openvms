package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestCredentialInstallHTTPSRequiresAuthenticatedIngress(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.20.0.0/24")}
	cases := []struct {
		name, remote, forwarded string
		duplicate               bool
		tls                     bool
		trusted                 bool
	}{
		{name: "untrusted spoofed proxy header", remote: "127.0.0.1:8080", forwarded: "https"},
		{name: "untrusted even if forwarded trust is configured elsewhere", remote: "192.0.2.8:8080", forwarded: "https"},
		{name: "trusted peer must assert https", remote: "10.20.0.7:8080"},
		{name: "multi-hop forwarded proto rejected", remote: "10.20.0.7:8080", forwarded: "https, https"},
		{name: "duplicate forwarded proto rejected", remote: "10.20.0.7:8080", forwarded: "https", duplicate: true},
		{name: "trusted proxy https accepted", remote: "10.20.0.7:8080", forwarded: "https", trusted: true},
		{name: "direct TLS accepted", remote: "192.0.2.8:8080", tls: true, trusted: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/servers/id/agent/install", nil)
			r.RemoteAddr = tc.remote
			if tc.forwarded != "" {
				r.Header.Set("X-Forwarded-Proto", tc.forwarded)
			}
			if tc.duplicate {
				r.Header.Add("X-Forwarded-Proto", "https")
			}
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			if got := credentialInstallHTTPSAllowed(r, trusted); got != tc.trusted {
				t.Fatalf("credentialInstallHTTPSAllowed() = %v, want %v", got, tc.trusted)
			}
		})
	}
}

const ingressTestPin = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestCredentialInstallHTTPSMiddlewareChecksPeerBeforeForwardedHeaders(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.20.0.7/32")}
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(http.StatusNoContent) })
	handler := credentialInstallHTTPS(trusted)(next)
	for _, tc := range []struct {
		name, remote, proto string
		want                int
	}{
		{"spoofed loopback header", "127.0.0.1:8080", "https", http.StatusForbidden},
		{"trusted peer over http", "10.20.0.7:8080", "http", http.StatusForbidden},
		{"trusted peer over https", "10.20.0.7:8080", "https", http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/servers/11111111-1111-1111-1111-111111111111/agent/install", strings.NewReader("{}"))
			r.RemoteAddr = tc.remote
			r.Header.Set("X-Forwarded-Proto", tc.proto)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	if calls != 1 {
		t.Fatalf("next handler calls=%d, want only trusted HTTPS", calls)
	}
}

func TestAgentInstallBodyRejectsUnknownTrailingAndOversizedJSON(t *testing.T) {
	for name, body := range map[string]string{
		"unknown fields":            `{"ssh_host":"192.0.2.44","ssh_port":22,"ssh_password":"secret","ssh_host_key_fingerprint":"` + ingressTestPin + `","binary":"not-allowed"}`,
		"removed fingerprint field": `{"ssh_host":"192.0.2.44","ssh_port":22,"ssh_password":"secret","ssh_host_key_fingerprint":"` + ingressTestPin + `"}`,
		"trailing JSON":             `{"ssh_host":"192.0.2.44","ssh_port":22,"ssh_password":"secret"} {}`,
		"oversized":                 `{"ssh_password":"` + strings.Repeat("x", agentInstallRequestMaxBytes) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			called := false
			h := validateAgentInstallBody(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			r := httptest.NewRequest(http.MethodPost, "/api/v1/servers/11111111-1111-1111-1111-111111111111/agent/install", strings.NewReader(body))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if called || w.Code != http.StatusBadRequest {
				t.Fatalf("called=%v status=%d", called, w.Code)
			}
			if strings.Contains(w.Body.String(), "secret") {
				t.Fatalf("error echoed request body: %s", w.Body.String())
			}
		})
	}
}

func TestAgentUpdateHTTPSIngressAndBodyBounds(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.20.0.7/32")}
	calls := 0
	handler := credentialInstallHTTPS(trusted)(validateAgentUpdateBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	})))
	for _, tc := range []struct {
		name, remote, proto, body string
		want                      int
	}{
		{"spoofed forwarded TLS", "127.0.0.1:8080", "https", `{"ssh_port":22,"ssh_password":"secret"}`, http.StatusForbidden},
		{"trusted forwarded TLS", "10.20.0.7:8080", "https", `{"ssh_port":22,"ssh_password":"secret"}`, http.StatusNoContent},
		{"unknown field", "10.20.0.7:8080", "https", `{"ssh_port":22,"ssh_password":"secret","host":"192.0.2.5"}`, http.StatusBadRequest},
		{"removed fingerprint field", "10.20.0.7:8080", "https", `{"ssh_port":22,"ssh_password":"secret","ssh_host_key_fingerprint":"` + ingressTestPin + `"}`, http.StatusBadRequest},
		{"trailing JSON", "10.20.0.7:8080", "https", `{"ssh_port":22,"ssh_password":"secret"} {}`, http.StatusBadRequest},
		{"oversized body", "10.20.0.7:8080", "https", `{"ssh_password":"` + strings.Repeat("x", agentInstallRequestMaxBytes) + `"}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/servers/11111111-1111-1111-1111-111111111111/agent/update-ssh", strings.NewReader(tc.body))
			r.RemoteAddr = tc.remote
			r.Header.Set("X-Forwarded-Proto", tc.proto)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.want != http.StatusNoContent && strings.Contains(w.Body.String(), "secret") {
				t.Fatalf("error echoed secret: %s", w.Body.String())
			}
		})
	}
	if calls != 1 {
		t.Fatalf("handler calls=%d, want only authenticated request", calls)
	}
}
