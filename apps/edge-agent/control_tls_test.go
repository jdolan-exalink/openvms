package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/platform/grpctls/grpctlstest"
)

func TestControlTLSFromEnv(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantTLS bool
		wantErr bool
	}{
		{name: "default is plaintext"},
		{name: "tls enabled", env: map[string]string{"OPENVMS_CONTROL_TLS": "true"}, wantTLS: true},
		{name: "tls enabled with server name", env: map[string]string{
			"OPENVMS_CONTROL_TLS": "1", "OPENVMS_CONTROL_TLS_SERVER_NAME": "ctl.example",
		}, wantTLS: true},
		{name: "invalid bool fails", env: map[string]string{"OPENVMS_CONTROL_TLS": "maybe"}, wantErr: true},
		{name: "CA file without TLS fails", env: map[string]string{"OPENVMS_CONTROL_TLS_CA_FILE": "/ca.pem"}, wantErr: true},
		{name: "server name without TLS fails", env: map[string]string{"OPENVMS_CONTROL_TLS_SERVER_NAME": "ctl.example"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(k string) string { return tc.env[k] }
			enabled, ca, name, err := controlTLSFromEnv(getenv)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if enabled != tc.wantTLS {
				t.Fatalf("enabled = %v, want %v", enabled, tc.wantTLS)
			}
			if ca != tc.env["OPENVMS_CONTROL_TLS_CA_FILE"] || name != tc.env["OPENVMS_CONTROL_TLS_SERVER_NAME"] {
				t.Fatalf("ca/name = %q/%q", ca, name)
			}
		})
	}
}

// The CA bundle is read at startup so a missing or broken file stops the agent
// instead of leaving it running without a control channel.
func TestControlTLSFromEnvValidatesCAFile(t *testing.T) {
	certFile, _ := grpctlstest.WriteSelfSigned(t)
	garbage := filepath.Join(t.TempDir(), "garbage.pem")
	if err := os.WriteFile(garbage, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		caFile  string
		wantErr bool
	}{
		{name: "readable CA", caFile: certFile},
		{name: "missing CA file fails", caFile: filepath.Join(t.TempDir(), "absent.pem"), wantErr: true},
		{name: "CA file without certificates fails", caFile: garbage, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"OPENVMS_CONTROL_TLS": "true", "OPENVMS_CONTROL_TLS_CA_FILE": tc.caFile}
			_, ca, _, err := controlTLSFromEnv(func(k string) string { return env[k] })
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && ca != tc.caFile {
				t.Fatalf("ca = %q, want %q", ca, tc.caFile)
			}
		})
	}
}
