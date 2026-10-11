package provision

import (
	"context"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func TestConfigFromRowRejectsInvalidStoredPort(t *testing.T) {
	for _, port := range []int32{-1, 0, 65536} {
		if got := configFromRow(db.ServerAgentTl{SecurePort: port}); got.SecurePort != 0 {
			t.Errorf("stored port %d became %d, want invalid zero", port, got.SecurePort)
		}
	}
}

func TestAgentTLSConfigValidation(t *testing.T) {
	cert, _ := testCA(t, "test", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	tests := []struct {
		name    string
		in      AgentTLSConfig
		wantErr bool
	}{
		{"system roots", AgentTLSConfig{SecurePort: 7443, TrustMode: AgentTLSTrustSystem}, false},
		{"custom CA", AgentTLSConfig{SecurePort: 7443, TrustMode: AgentTLSTrustCustom, CAPEM: ca}, false},
		{"missing port", AgentTLSConfig{TrustMode: AgentTLSTrustSystem}, true},
		{"invalid port", AgentTLSConfig{SecurePort: 65536, TrustMode: AgentTLSTrustSystem}, true},
		{"missing custom CA", AgentTLSConfig{SecurePort: 7443, TrustMode: AgentTLSTrustCustom}, true},
		{"system plus CA", AgentTLSConfig{SecurePort: 7443, TrustMode: AgentTLSTrustSystem, CAPEM: ca}, true},
		{"invalid mode", AgentTLSConfig{SecurePort: 7443, TrustMode: "other"}, true},
		{"invalid CA", AgentTLSConfig{SecurePort: 7443, TrustMode: AgentTLSTrustCustom, CAPEM: []byte("bad")}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateAgentTLSConfig("192.0.2.3", tt.in); (err != nil) != tt.wantErr {
				t.Fatalf("validateAgentTLSConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSetAgentTLSConfigAuthorizesBeforeStoreAccess(t *testing.T) {
	serverID := uuid.New()
	called := false
	svc := &Service{requireServerManage: func(context.Context, authz.Actor, uuid.UUID) error {
		called = true
		return errors.New("denied")
	}}
	if _, err := svc.SetAgentTLSConfig(context.Background(), authz.Actor{}, serverID, AgentTLSConfig{SecurePort: 7443, TrustMode: AgentTLSTrustSystem}); err == nil || err.Error() != "denied" {
		t.Fatalf("SetAgentTLSConfig error = %v, want denied", err)
	}
	if !called {
		t.Fatal("authorization was not checked")
	}
}
