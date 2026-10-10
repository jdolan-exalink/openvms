package main

import "testing"

func TestControlTLSFromEnv(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantTLS bool
		wantErr bool
	}{
		{name: "default is plaintext"},
		{name: "tls enabled", env: map[string]string{"OPENVMS_CONTROL_TLS": "true"}, wantTLS: true},
		{name: "tls enabled with CA and name", env: map[string]string{
			"OPENVMS_CONTROL_TLS": "1", "OPENVMS_CONTROL_TLS_CA_FILE": "/ca.pem", "OPENVMS_CONTROL_TLS_SERVER_NAME": "ctl.example",
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
