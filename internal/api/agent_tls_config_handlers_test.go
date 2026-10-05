package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/provision"
)

func TestAgentTLSConfigPUTRejectsNonStrictJSON(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router, err := NewRouter(&Handlers{Log: log}, log, Options{})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		body string
	}{
		{"unknown private key field", `{"secure_port":7443,"trust_mode":"system","private_key":"secret"}`},
		{"trailing JSON value", `{"secure_port":7443,"trust_mode":"system"}{}`},
		{"oversized body", `{"secure_port":7443,"trust_mode":"system","ca_pem":"` + strings.Repeat("A", agentTLSRequestMaxBytes+1) + `"}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/v1/servers/11111111-1111-1111-1111-111111111111/agent/tls", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rw := httptest.NewRecorder()
			router.ServeHTTP(rw, req)
			if rw.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s; want bad request", rw.Code, rw.Body.String())
			}
		})
	}
}

func TestAgentTLSConfigProjectionUsesPEMString(t *testing.T) {
	ca := "-----BEGIN CERTIFICATE-----\npublic-ca\n-----END CERTIFICATE-----\n"
	got := agentTLSConfig(provision.AgentTLSConfig{SecurePort: 7443, TrustMode: provision.AgentTLSTrustCustom, CAPEM: []byte(ca)})
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"ca_pem":"-----BEGIN CERTIFICATE-----`) || strings.Contains(string(encoded), "private_key") {
		t.Fatalf("unexpected config projection: %s", encoded)
	}
	var roundTrip gen.ServerAgentTLSConfig
	if err := json.Unmarshal(encoded, &roundTrip); err != nil || roundTrip.CaPem == nil || *roundTrip.CaPem != ca {
		t.Fatalf("PEM was not represented as a JSON string: %#v err=%v", roundTrip, err)
	}
}

func TestProvisionAgentTLSConfigValidatesTrustExclusivity(t *testing.T) {
	ca := "-----BEGIN CERTIFICATE-----\npublic-ca\n-----END CERTIFICATE-----\n"
	cases := []struct {
		name    string
		in      gen.ServerAgentTLSConfig
		wantErr bool
	}{
		{"system roots", gen.ServerAgentTLSConfig{SecurePort: 7443, TrustMode: gen.ServerAgentTLSConfigTrustModeSystem}, false},
		{"custom CA", gen.ServerAgentTLSConfig{SecurePort: 7443, TrustMode: gen.ServerAgentTLSConfigTrustModeCustom, CaPem: &ca}, false},
		{"system with CA", gen.ServerAgentTLSConfig{SecurePort: 7443, TrustMode: gen.ServerAgentTLSConfigTrustModeSystem, CaPem: &ca}, true},
		{"custom without CA", gen.ServerAgentTLSConfig{SecurePort: 7443, TrustMode: gen.ServerAgentTLSConfigTrustModeCustom}, true},
		{"bad port", gen.ServerAgentTLSConfig{SecurePort: 65536, TrustMode: gen.ServerAgentTLSConfigTrustModeSystem}, true},
		{"invalid trust mode", gen.ServerAgentTLSConfig{SecurePort: 7443, TrustMode: "other"}, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := provisionAgentTLSConfig(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("provisionAgentTLSConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAgentTLSConfigRoutesRequireActor(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router, err := NewRouter(&Handlers{Log: log}, log, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, method, body string
	}{
		{"get", http.MethodGet, ""},
		{"set", http.MethodPut, `{"secure_port":7443,"trust_mode":"system"}`},
		{"delete", http.MethodDelete, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/servers/11111111-1111-1111-1111-111111111111/agent/tls", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rw := httptest.NewRecorder()
			router.ServeHTTP(rw, req)
			if rw.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d body=%s; want unauthorized", rw.Code, rw.Body.String())
			}
		})
	}
}

func TestAgentTLSConfigPUTRejectsInvalidTrustShapes(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router, err := NewRouter(&Handlers{Log: log}, log, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"secure_port":7443,"trust_mode":"system","ca_pem":""}`,
		`{"secure_port":7443,"trust_mode":"custom"}`,
		`{"secure_port":7443,"trust_mode":"unknown"}`,
		`{"secure_port":0,"trust_mode":"system"}`,
		`{"secure_port":7443,"trust_mode":"custom","ca_pem":"` + strings.Repeat("A", agentTLSPemMaxBytes+1) + `"}`,
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/servers/11111111-1111-1111-1111-111111111111/agent/tls", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, req)
		if rw.Code != http.StatusBadRequest {
			t.Errorf("body %s: status=%d want 400", body, rw.Code)
		}
	}
}
