package config

import "testing"

func TestLoadGRPCAgentListener(t *testing.T) {
	cases := []struct {
		name, addr, cert, key string
		wantErr               bool
		wantAddr              string
	}{
		{name: "unset keeps the agent listener disabled"},
		{name: "unset stays disabled with TLS configured", cert: "/c.pem", key: "/k.pem"},
		{name: "addr with server TLS enables it", addr: ":9443", cert: "/c.pem", key: "/k.pem", wantAddr: ":9443"},
		{name: "addr without server TLS is refused", addr: ":9443", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://x")
			t.Setenv("GRPC_TLS_CERT_FILE", tc.cert)
			t.Setenv("GRPC_TLS_KEY_FILE", tc.key)
			t.Setenv("GRPC_AGENT_ADDR", tc.addr)
			cfg, err := Load("test")
			if (err != nil) != tc.wantErr {
				t.Fatalf("Load error = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && cfg.GRPCAgentAddr != tc.wantAddr {
				t.Fatalf("GRPCAgentAddr = %q, want %q", cfg.GRPCAgentAddr, tc.wantAddr)
			}
		})
	}
}
