package config

import "testing"

func TestLoadAgentInstallEnrollment(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantErr bool
		want    AgentEnrollConfig
	}{
		{name: "unset leaves installs unchanged"},
		{name: "unset with the listener configured also leaves installs unchanged", env: map[string]string{"GRPC_AGENT_ADDR": ":9443"}},
		{
			name: "both public settings with the listener enable it",
			env:  map[string]string{"GRPC_AGENT_ADDR": ":9443", "AGENT_GRPC_PUBLIC_ADDR": "vms.example.com:9443", "AGENT_API_PUBLIC_URL": "https://vms.example.com"},
			want: AgentEnrollConfig{GRPCAddr: "vms.example.com:9443", APIURL: "https://vms.example.com"},
		},
		{
			name: "optional trust settings",
			env: map[string]string{"GRPC_AGENT_ADDR": ":9443", "AGENT_GRPC_PUBLIC_ADDR": "10.0.0.5:9443", "AGENT_API_PUBLIC_URL": "https://10.0.0.5",
				"AGENT_GRPC_CA_FILE": "/etc/ca.pem", "AGENT_GRPC_SERVER_NAME": "vms.example.com", "AGENT_API_CA_FILE": "/etc/api-ca.pem"},
			want: AgentEnrollConfig{GRPCAddr: "10.0.0.5:9443", APIURL: "https://10.0.0.5", GRPCCAFile: "/etc/ca.pem", ServerName: "vms.example.com", APICAFile: "/etc/api-ca.pem"},
		},
		{name: "public address without the listener", env: map[string]string{"AGENT_GRPC_PUBLIC_ADDR": "vms:9443", "AGENT_API_PUBLIC_URL": "https://vms"}, wantErr: true},
		{name: "public address without the API URL", env: map[string]string{"GRPC_AGENT_ADDR": ":9443", "AGENT_GRPC_PUBLIC_ADDR": "vms:9443"}, wantErr: true},
		{name: "API URL without the public address", env: map[string]string{"GRPC_AGENT_ADDR": ":9443", "AGENT_API_PUBLIC_URL": "https://vms"}, wantErr: true},
		{name: "plain http API URL", env: map[string]string{"GRPC_AGENT_ADDR": ":9443", "AGENT_GRPC_PUBLIC_ADDR": "vms:9443", "AGENT_API_PUBLIC_URL": "http://vms"}, wantErr: true},
		{name: "address without a port", env: map[string]string{"GRPC_AGENT_ADDR": ":9443", "AGENT_GRPC_PUBLIC_ADDR": "vms", "AGENT_API_PUBLIC_URL": "https://vms"}, wantErr: true},
		{name: "trust settings without the public address", env: map[string]string{"GRPC_AGENT_ADDR": ":9443", "AGENT_GRPC_CA_FILE": "/ca.pem"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://x")
			t.Setenv("GRPC_TLS_CERT_FILE", "/c.pem")
			t.Setenv("GRPC_TLS_KEY_FILE", "/k.pem")
			for _, k := range []string{"GRPC_AGENT_ADDR", "AGENT_GRPC_PUBLIC_ADDR", "AGENT_API_PUBLIC_URL", "AGENT_GRPC_CA_FILE", "AGENT_GRPC_SERVER_NAME", "AGENT_API_CA_FILE"} {
				t.Setenv(k, tc.env[k])
			}
			cfg, err := Load("test")
			if (err != nil) != tc.wantErr {
				t.Fatalf("Load error = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && cfg.AgentEnroll != tc.want {
				t.Fatalf("AgentEnroll = %+v, want %+v", cfg.AgentEnroll, tc.want)
			}
		})
	}
}
