package config

import "testing"

func TestLoadGRPCTLS(t *testing.T) {
	cases := []struct {
		name, cert, key string
		wantErr         bool
		wantTLS         bool
	}{
		{name: "neither set stays plaintext"},
		{name: "both set enables TLS", cert: "/c.pem", key: "/k.pem", wantTLS: true},
		{name: "cert without key fails", cert: "/c.pem", wantErr: true},
		{name: "key without cert fails", key: "/k.pem", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://x")
			t.Setenv("GRPC_TLS_CERT_FILE", tc.cert)
			t.Setenv("GRPC_TLS_KEY_FILE", tc.key)
			cfg, err := Load("test")
			if (err != nil) != tc.wantErr {
				t.Fatalf("Load error = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && cfg.GRPCTLSEnabled() != tc.wantTLS {
				t.Fatalf("GRPCTLSEnabled = %v, want %v", cfg.GRPCTLSEnabled(), tc.wantTLS)
			}
		})
	}
}
