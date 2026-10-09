package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/platform/buildinfo"
)

func TestLoadAgentTLSConfig(t *testing.T) {
	certFile, keyFile := writeTestKeyPair(t)
	_, otherKeyFile := writeTestKeyPair(t)
	badCertFile := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(badCertFile, []byte("not a PEM certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		listen     string
		cert       string
		key        string
		wantEnable bool
		wantErr    bool
	}{
		{name: "disabled when all settings absent"},
		{name: "enabled with complete valid settings", listen: "127.0.0.1:9443", cert: certFile, key: keyFile, wantEnable: true},
		{name: "missing cert fails closed", listen: "127.0.0.1:9443", key: keyFile, wantErr: true},
		{name: "missing key fails closed", listen: "127.0.0.1:9443", cert: certFile, wantErr: true},
		{name: "missing listen fails closed", cert: certFile, key: keyFile, wantErr: true},
		{name: "invalid listen fails closed", listen: "not-an-address", cert: certFile, key: keyFile, wantErr: true},
		{name: "invalid cert fails closed", listen: "127.0.0.1:9443", cert: badCertFile, key: keyFile, wantErr: true},
		{name: "unreadable key fails closed", listen: "127.0.0.1:9443", cert: certFile, key: filepath.Join(t.TempDir(), "missing.pem"), wantErr: true},
		{name: "mismatched key fails closed", listen: "127.0.0.1:9443", cert: certFile, key: otherKeyFile, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, enabled, err := loadAgentTLSConfig(tt.listen, tt.cert, tt.key)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if enabled != tt.wantEnable {
				t.Fatalf("enabled = %v, want %v", enabled, tt.wantEnable)
			}
			if enabled && (cfg.Addr != tt.listen || len(cfg.Certificates) != 1) {
				t.Fatalf("unexpected config: %#v", cfg)
			}
		})
	}
}

func TestAgentTLSHealthRequiresBearerAndExposesBuildMetadata(t *testing.T) {
	info := agentHealthInfo{
		Status:  "ok",
		Version: "0.1.0",
		Build:   agentBuildMetadata{Version: "v-test", Commit: "abc123", BuildTime: "2026-10-06T12:00:00Z", GoVersion: "go-test"},
		Variant: "openvino",
	}
	health := newAgentHealthHandler("agent-secret", info)
	mux := buildTLSMux(health, nil, nil)
	for _, token := range []string{"", "wrong-token"} {
		r := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("unauthorized health status=%d token=%q, want 401", w.Code, token)
		}
		if strings.Contains(w.Body.String(), info.Build.Commit) || strings.Contains(w.Body.String(), info.Build.Version) {
			t.Errorf("unauthorized health response exposed build metadata: %q", w.Body.String())
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	r.Header.Set("Authorization", "Bearer agent-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("authorized health status=%d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("health content type=%q, want application/json", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("health cache-control=%q, want no-store", got)
	}
	var got agentHealthInfo
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got != info {
		t.Fatalf("health metadata=%+v, want %+v", got, info)
	}
	if strings.Contains(w.Body.String(), "agent-secret") {
		t.Fatal("health response exposed the bearer token")
	}
	r = httptest.NewRequest(http.MethodPost, "/v1/health", nil)
	r.Header.Set("Authorization", "Bearer agent-secret")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong-method health status=%d, want 405", w.Code)
	}

	plain := buildMux("agent-secret", "openvino", nil, nil, func(*http.Request) error { return nil }, func() {})
	r = httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	r.Header.Set("Authorization", "Bearer agent-secret")
	w = httptest.NewRecorder()
	plain.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("plaintext health status=%d, want 404", w.Code)
	}
}

func TestCurrentAgentHealthInfoUsesBuiltMetadata(t *testing.T) {
	got := currentAgentHealthInfo("nvidia")
	if got.Status != "ok" || got.Version != agent.Version || got.Variant != "nvidia" {
		t.Fatalf("health identity = %+v", got)
	}
	wantBuild := agentBuildMetadata{Version: buildinfo.Version, Commit: buildinfo.Commit, BuildTime: buildinfo.BuildTime, GoVersion: buildinfo.GoVersion()}
	if got.Build != wantBuild {
		t.Fatalf("build metadata = %+v, want %+v", got.Build, wantBuild)
	}
	if got.BinaryIdentity.SHA256 == "" || got.BinaryIdentity.Architecture == "" {
		t.Fatalf("health omitted available executable identity: %+v", got.BinaryIdentity)
	}
}

func TestAgentTLSHealthBoundsMetadataResponse(t *testing.T) {
	info := currentAgentHealthInfo(strings.Repeat("x", 4096))
	mux := buildTLSMux(newAgentHealthHandler("agent-secret", info), nil, nil)
	r := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	r.Header.Set("Authorization", "Bearer agent-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("oversized health metadata status=%d, want 500", w.Code)
	}
	if len(w.Body.Bytes()) > 128 || strings.Contains(w.Body.String(), strings.Repeat("x", 64)) {
		t.Fatalf("oversized health response was not bounded: %d bytes", w.Body.Len())
	}
}

func TestAgentTLSHealthRejectsPayloadAtLimitWhenNewlineExceedsLimit(t *testing.T) {
	info := currentAgentHealthInfo("")
	baseline, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	info.Variant = strings.Repeat("x", agentHealthMaxResponseBytes-len(baseline))
	payload, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) != agentHealthMaxResponseBytes {
		t.Fatalf("JSON payload length = %d, want exact limit %d", len(payload), agentHealthMaxResponseBytes)
	}

	mux := buildTLSMux(newAgentHealthHandler("agent-secret", info), nil, nil)
	r := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	r.Header.Set("Authorization", "Bearer agent-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("health response status = %d, want 500 because newline exceeds limit", w.Code)
	}
	if w.Body.Len() > 128 {
		t.Fatalf("oversized health response body = %d bytes, want bounded error", w.Body.Len())
	}
}

func TestAgentTLSMuxExposesProbeWithoutChangingHTTPMux(t *testing.T) {
	discovery := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	probe := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	mux := buildTLSMux(nil, discovery, probe)
	for _, path := range []string{"/v1/metrics", "/v1/update"} {
		r := httptest.NewRequest("POST", path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/v1/onvif/discover", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("discovery status = %d, want 204", w.Code)
	}
	r = httptest.NewRequest("POST", "/v1/onvif/probe", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("TLS probe status = %d, want 202", w.Code)
	}

	plainMux := buildMux("agent-secret", "", nil, discovery, func(*http.Request) error { return nil }, func() {})
	r = httptest.NewRequest("POST", "/v1/onvif/probe", nil)
	w = httptest.NewRecorder()
	plainMux.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("plain HTTP probe status = %d, want 404", w.Code)
	}
}

func TestAgentTLSServerRequiresTLS12(t *testing.T) {
	certFile, keyFile := writeTestKeyPair(t)
	cfg, enabled, err := loadAgentTLSConfig("127.0.0.1:9443", certFile, keyFile)
	if err != nil || !enabled {
		t.Fatalf("load config: enabled=%v err=%v", enabled, err)
	}
	server := newAgentTLSServer(cfg, nil, nil, nil)
	if server.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x, want TLS 1.2", server.TLSConfig.MinVersion)
	}
	if len(server.TLSConfig.Certificates) != 1 {
		t.Fatalf("certificate count = %d, want 1", len(server.TLSConfig.Certificates))
	}
}

func TestAgentListenerFailureShutsDownBothServers(t *testing.T) {
	plain := &http.Server{Addr: "plain"}
	tlsServer := &http.Server{Addr: "tls", TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	stop := make(chan struct{})
	serveCalls := make(chan *http.Server, 2)
	wantErr := errors.New("injected listen failure")
	var mu sync.Mutex
	shutdownCalls := map[*http.Server]bool{}
	err := serveAgentServersWith(slog.New(slog.NewTextHandler(io.Discard, nil)), func(server *http.Server) error {
		serveCalls <- server
		if server == plain {
			return wantErr
		}
		<-stop
		return http.ErrServerClosed
	}, func(_ context.Context, server *http.Server) error {
		mu.Lock()
		shutdownCalls[server] = true
		if len(shutdownCalls) == 2 {
			close(stop)
		}
		mu.Unlock()
		return nil
	}, plain, tlsServer)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want injected failure", err)
	}
	for range 2 {
		<-serveCalls
	}
	if !shutdownCalls[plain] || !shutdownCalls[tlsServer] {
		t.Fatalf("shutdown calls = %#v", shutdownCalls)
	}
}

func writeTestKeyPair(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: nil, DNSNames: []string{"localhost"}}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}
