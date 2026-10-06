package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func validAgentUpdateStartRequest() AgentUpdateStartRequest {
	return AgentUpdateStartRequest{SSHPort: 2222, Password: "transient-root-password", HostKey: agentInstallTestPin}
}

func TestStartAgentUpdateDeniesBeforeAgentSecretArtifactOrSSH(t *testing.T) {
	var calls []string
	svc := &Service{
		requireAgentUpdatePermissions: func(context.Context, authz.Actor, uuid.UUID) error {
			calls = append(calls, "authorize")
			return errors.New("denied")
		},
		loadAgentUpdateRegistration: func(context.Context, authz.Actor, uuid.UUID) (db.ServerAgent, string, error) {
			calls = append(calls, "registration")
			return db.ServerAgent{}, "", nil
		},
		agentUpdateBinary:      func() ([]byte, error) { calls = append(calls, "artifact"); return []byte("binary"), nil },
		agentUpdateCredentials: func(string) ([]byte, []byte, error) { calls = append(calls, "credentials"); return nil, nil, nil },
		agentUpdateRun: func(context.Context, AgentUpdateRequest, func(string)) error {
			calls = append(calls, "ssh")
			return nil
		},
	}
	_, err := svc.StartServerAgentUpdate(context.Background(), authz.Actor{UserID: uuid.New()}, uuid.New(), validAgentUpdateStartRequest())
	if err == nil || err.Error() != "denied" || fmt.Sprint(calls) != "[authorize]" {
		t.Fatalf("denial err=%v calls=%v", err, calls)
	}
}

func TestStartAgentUpdateUsesRegisteredHostAndPersistsTrustOnlyAfterVerifiedRunner(t *testing.T) {
	actor, serverID := authz.Actor{UserID: uuid.New()}, uuid.New()
	const token = "existing-sealed-agent-bearer-token-value"
	ca, key := newAgentInstallTLS(t, "192.0.2.44")
	var runRequest AgentUpdateRequest
	var persisted int
	svc := &Service{
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		requireAgentUpdatePermissions:  func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		loadAgentUpdateRegistration: func(_ context.Context, _ authz.Actor, id uuid.UUID) (db.ServerAgent, string, error) {
			if id != serverID {
				t.Fatalf("unexpected target %s", id)
			}
			return db.ServerAgent{ServerID: serverID, TenantID: uuid.New(), Host: "192.0.2.44", Port: 7419, TokenSealed: []byte("sealed")}, token, nil
		},
		agentUpdateTLSExists: func(context.Context, authz.Actor, uuid.UUID) (bool, error) { return false, nil },
		agentUpdateBinary:    func() ([]byte, error) { return []byte("trusted-binary"), nil },
		agentUpdateCredentials: func(host string) ([]byte, []byte, error) {
			if host != "192.0.2.44" {
				t.Fatalf("TLS SAN host=%q", host)
			}
			return ca, key, nil
		},
		agentUpdateRun: func(_ context.Context, request AgentUpdateRequest, progress func(string)) error {
			runRequest = request
			progress("activating")
			return nil
		},
		agentUpdatePersistTLS: func(_ context.Context, _ authz.Actor, original db.ServerAgent, port uint16, gotCA []byte) error {
			persisted++
			if original.ServerID != serverID || original.Host != "192.0.2.44" || original.Port != 7419 || string(original.TokenSealed) != "sealed" || port != agentInstallHTTPSPort || !bytes.Equal(gotCA, ca) {
				t.Fatalf("unexpected persisted TLS target/config")
			}
			return nil
		},
	}
	job, err := svc.StartServerAgentUpdate(context.Background(), actor, serverID, validAgentUpdateStartRequest())
	if err != nil {
		t.Fatal(err)
	}
	final := waitAgentUpdateJob(t, svc, actor, serverID, job)
	if final.Status != "succeeded" || persisted != 1 {
		t.Fatalf("job=%+v persisted=%d", final, persisted)
	}
	if runRequest.Host != "192.0.2.44" || runRequest.AgentToken != token || runRequest.Username != "root" || runRequest.SSHPort != 2222 {
		t.Fatalf("runner request did not use stored host/token: %+v", runRequest)
	}
	if strings.Contains(fmt.Sprintf("%+v", runRequest), validAgentUpdateStartRequest().Password) {
		t.Fatal("runner request formatter leaked password")
	}
	encoded, _ := json.Marshal(final)
	if strings.Contains(string(encoded), token) || strings.Contains(string(encoded), validAgentUpdateStartRequest().Password) {
		t.Fatalf("job leaked credentials: %s", encoded)
	}
}

func TestStartAgentUpdateRunnerFailureDoesNotPersistTLS(t *testing.T) {
	actor, serverID := authz.Actor{UserID: uuid.New()}, uuid.New()
	ca, key := newAgentInstallTLS(t, "192.0.2.44")
	persisted := false
	svc := &Service{
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		requireAgentUpdatePermissions:  func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		loadAgentUpdateRegistration: func(context.Context, authz.Actor, uuid.UUID) (db.ServerAgent, string, error) {
			return db.ServerAgent{ServerID: serverID, Host: "192.0.2.44", Port: 7419}, "existing-token-value-0123456789", nil
		},
		agentUpdateTLSExists:   func(context.Context, authz.Actor, uuid.UUID) (bool, error) { return false, nil },
		agentUpdateBinary:      func() ([]byte, error) { return []byte("binary"), nil },
		agentUpdateCredentials: func(string) ([]byte, []byte, error) { return ca, key, nil },
		agentUpdateRun: func(context.Context, AgentUpdateRequest, func(string)) error {
			return errors.New("remote output and password")
		},
		agentUpdatePersistTLS: func(context.Context, authz.Actor, db.ServerAgent, uint16, []byte) error {
			persisted = true
			return nil
		},
	}
	job, err := svc.StartServerAgentUpdate(context.Background(), actor, serverID, validAgentUpdateStartRequest())
	if err != nil {
		t.Fatal(err)
	}
	final := waitAgentUpdateJob(t, svc, actor, serverID, job)
	if final.Status != "failed" || persisted {
		t.Fatalf("job=%+v persisted=%v", final, persisted)
	}
	if strings.Contains(final.Message, "password") || strings.Contains(final.Message, "remote output") {
		t.Fatalf("job exposed runner error: %+v", final)
	}
}

func TestStartAgentUpdateRefusesExistingTLSAndSharesInstallReservation(t *testing.T) {
	actor, serverID := authz.Actor{UserID: uuid.New()}, uuid.New()
	var artifactReads int
	svc := &Service{
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		requireAgentUpdatePermissions:  func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		loadAgentUpdateRegistration: func(context.Context, authz.Actor, uuid.UUID) (db.ServerAgent, string, error) {
			return db.ServerAgent{ServerID: serverID, Host: "192.0.2.44", Port: 7419}, "existing-token-value-0123456789", nil
		},
		agentUpdateTLSExists: func(context.Context, authz.Actor, uuid.UUID) (bool, error) { return true, nil },
		agentUpdateBinary:    func() ([]byte, error) { artifactReads++; return []byte("binary"), nil },
		agentUpdateRun: func(context.Context, AgentUpdateRequest, func(string)) error {
			t.Fatal("runner invoked for TLS-enabled registration")
			return nil
		},
	}
	if _, err := svc.StartServerAgentUpdate(context.Background(), actor, serverID, validAgentUpdateStartRequest()); err != ErrAgentUpdateTLSAlreadyConfigured {
		t.Fatalf("existing TLS update error = %v", err)
	}
	if artifactReads != 0 {
		t.Fatalf("artifact was read for unsupported TLS replacement: %d", artifactReads)
	}
	// The shared per-server reservation prevents install and update from racing.
	job, err := svc.reserveAgentInstallJob(actor.UserID, serverID)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.releaseAgentInstallJob(job, true)
	if _, err := svc.StartServerAgentUpdate(context.Background(), actor, serverID, validAgentUpdateStartRequest()); err != ErrAgentInstallBusy {
		t.Fatalf("update did not share install reservation: %v", err)
	}
}

func TestStartAgentUpdateRejectsRegistrationForDifferentServerBeforeArtifact(t *testing.T) {
	actor, serverID := authz.Actor{UserID: uuid.New()}, uuid.New()
	artifactReads := 0
	svc := &Service{
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		requireAgentUpdatePermissions:  func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		loadAgentUpdateRegistration: func(context.Context, authz.Actor, uuid.UUID) (db.ServerAgent, string, error) {
			return db.ServerAgent{ServerID: uuid.New(), Host: "192.0.2.44", Port: 7419}, "existing-token-value-0123456789", nil
		},
		agentUpdateBinary: func() ([]byte, error) { artifactReads++; return []byte("binary"), nil },
	}
	if _, err := svc.StartServerAgentUpdate(context.Background(), actor, serverID, validAgentUpdateStartRequest()); err != ErrAgentUpdateUnavailable {
		t.Fatalf("mismatched registration error = %v", err)
	}
	if artifactReads != 0 {
		t.Fatalf("artifact read for mismatched registration: %d", artifactReads)
	}
}

func TestAgentUpdateStartRequestFormattingRedactsPassword(t *testing.T) {
	request := validAgentUpdateStartRequest()
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{request.String(), fmt.Sprintf("%+v", request), fmt.Sprintf("%#v", request), string(encoded)} {
		if strings.Contains(rendered, request.Password) {
			t.Fatalf("request serialization leaked password: %s", rendered)
		}
	}
}

func waitAgentUpdateJob(t *testing.T, svc *Service, actor authz.Actor, serverID uuid.UUID, job AgentInstallJob) AgentInstallJob {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, err := svc.GetServerAgentInstallJob(context.Background(), actor, serverID, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == "succeeded" || current.Status == "failed" {
			return current
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("agent update job did not finish")
	return AgentInstallJob{}
}
