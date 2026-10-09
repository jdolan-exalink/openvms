package provision

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func TestAgentUninstallStartRequestRedactsPassword(t *testing.T) {
	req := AgentUninstallStartRequest{SSHPort: 22, Password: "super-secret-root-password"}
	if strings.Contains(req.String(), "super-secret") {
		t.Fatalf("String() must redact password: %s", req.String())
	}
	if strings.Contains(req.GoString(), "super-secret") {
		t.Fatalf("GoString() must redact password: %s", req.GoString())
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "super-secret") {
		t.Fatalf("MarshalJSON() must redact password: %s", string(b))
	}
}

func TestStartServerAgentUninstallValidation(t *testing.T) {
	svc := &Service{}
	actor := authz.Actor{UserID: uuid.New()}
	serverID := uuid.New()

	// Missing server ID
	if _, err := svc.StartServerAgentUninstall(context.Background(), actor, uuid.Nil, AgentUninstallStartRequest{SSHPort: 22, Password: "123"}); err == nil {
		t.Fatal("expected error for nil server ID")
	}

	// Missing port
	if _, err := svc.StartServerAgentUninstall(context.Background(), actor, serverID, AgentUninstallStartRequest{SSHPort: 0, Password: "123"}); err == nil {
		t.Fatal("expected error for port 0")
	}

	// Missing password
	if _, err := svc.StartServerAgentUninstall(context.Background(), actor, serverID, AgentUninstallStartRequest{SSHPort: 22, Password: ""}); err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestStartServerAgentUninstallAuthorizationDenial(t *testing.T) {
	calledRegistration := false
	svc := &Service{
		requireAgentUpdatePermissions: func(context.Context, authz.Actor, uuid.UUID) error {
			return errors.New("unauthorized")
		},
		loadAgentUpdateRegistration: func(context.Context, authz.Actor, uuid.UUID) (db.ServerAgent, string, error) {
			calledRegistration = true
			return db.ServerAgent{}, "", nil
		},
	}
	actor := authz.Actor{UserID: uuid.New()}
	serverID := uuid.New()
	_, err := svc.StartServerAgentUninstall(context.Background(), actor, serverID, AgentUninstallStartRequest{SSHPort: 22, Password: "secret"})
	if err == nil || err.Error() != "unauthorized" {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	if calledRegistration {
		t.Fatal("registration must not be loaded when authorization is denied")
	}
}
