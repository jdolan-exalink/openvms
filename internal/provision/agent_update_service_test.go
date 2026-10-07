package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func validAgentUpdateStartRequest() AgentUpdateStartRequest {
	return AgentUpdateStartRequest{SSHPort: 2222, Password: "transient-root-password"}
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
	var trustStored int
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
		agentSSHHostKeyPersist: func(_ context.Context, gotActor authz.Actor, gotServer uuid.UUID, host string, port uint16, fingerprint string) error {
			trustStored++
			if gotActor.UserID != actor.UserID || gotServer != serverID || host != "192.0.2.44" || port != 2222 || fingerprint != agentInstallTestPin {
				t.Fatal("host-key trust callback was not bound to registered update target")
			}
			return nil
		},
		agentUpdateRun: func(ctx context.Context, request AgentUpdateRequest, progress func(string)) error {
			runRequest = request
			if request.hostKeyTrust == nil {
				return errors.New("missing first-contact trust callback")
			}
			if err := request.hostKeyTrust(ctx, agentInstallTestPin); err != nil {
				return err
			}
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
	if runRequest.Host != "192.0.2.44" || runRequest.AgentToken != token || runRequest.Username != "root" || runRequest.SSHPort != 2222 || runRequest.ExpectedHostKey != "" || runRequest.hostKeyTrust == nil || trustStored != 1 {
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

func TestRunServerAgentUpdateLogsOnlySafeFailureDiagnostics(t *testing.T) {
	const marker = "root-password-secret remote-stderr token-secret 203.0.113.77 private-key-secret"
	tests := []struct {
		name         string
		run          func() error
		persistErr   error
		wantStage    string
		wantCode     string
		wantRollback string
		wantMessage  string
	}{
		{
			name: "typed runner error",
			run: func() error {
				return &agentUpdateFailure{stage: "verifying_health", code: "health_check_failed", rollback: "restored"}
			},
			wantStage: "verifying_health", wantCode: "health_check_failed", wantRollback: "restored", wantMessage: "HTTPS health verification failed; the previous agent files were restored.",
		},
		{
			name:      "unknown nested error",
			run:       func() error { return errors.New(marker) },
			wantStage: "unknown", wantCode: "update_failed", wantRollback: "unknown", wantMessage: "Agent update failed; inspect the target before retrying.",
		},
		{
			name: "invalid diagnostic enum",
			run: func() error {
				return &agentUpdateFailure{stage: marker, code: marker, rollback: marker}
			},
			wantStage: "unknown", wantCode: "update_failed", wantRollback: "unknown", wantMessage: "Agent update failed; inspect the target before retrying.",
		},
		{
			name:      "panic",
			run:       func() error { panic(marker) },
			wantStage: "unknown", wantCode: "update_failed", wantRollback: "unknown", wantMessage: "Agent update failed; inspect the target before retrying.",
		},
		{
			name: "TLS registration persistence failure",
			run:  func() error { return nil }, persistErr: errors.New(marker),
			wantStage: "registration", wantCode: "tls_trust_registration_failed", wantRollback: "not_attempted", wantMessage: "Agent HTTPS health succeeded, but trust registration failed; inspect server configuration before retrying.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			serverID, jobID := uuid.New(), uuid.New()
			password := "request-password-secret"
			token := "registered-agent-token-secret"
			service := &Service{
				Log:                   slog.New(slog.NewTextHandler(&output, nil)),
				agentUpdateRun:        func(context.Context, AgentUpdateRequest, func(string)) error { return test.run() },
				agentUpdatePersistTLS: func(context.Context, authz.Actor, db.ServerAgent, uint16, []byte) error { return test.persistErr },
			}
			job := &agentInstallJob{snapshot: AgentInstallJob{ID: jobID}}
			original := db.ServerAgent{ServerID: serverID, Host: "192.0.2.55", TokenSealed: []byte("sealed-token")}
			fingerprint := "SHA256:diagnostic-fingerprint-secret"
			request := AgentUpdateRequest{Host: original.Host, SSHPort: 2222, Username: "root", Password: password, ExpectedHostKey: fingerprint, AgentToken: token, SecurePort: 7443, TLSCertificate: []byte("public-cert"), TLSPrivateKey: []byte("private-key-secret"), Binary: []byte("trusted-binary")}
			service.runServerAgentUpdate(authz.Actor{UserID: uuid.New()}, job, original, request)

			logOutput := output.String()
			for _, want := range []string{serverID.String(), jobID.String(), "server_id", "job_id", "stage=" + test.wantStage, "code=" + test.wantCode, "rollback=" + test.wantRollback} {
				if !strings.Contains(logOutput, want) {
					t.Errorf("safe log missing %q: %s", want, logOutput)
				}
			}
			for _, secret := range []string{marker, password, token, original.Host, "root", fingerprint, "public-cert", "private-key-secret", "trusted-binary", "sealed-token"} {
				if strings.Contains(logOutput, secret) {
					t.Errorf("log contains private value %q: %s", secret, logOutput)
				}
			}
			final := job.read()
			if final.Status != "failed" || !strings.Contains(final.Message, test.wantMessage) {
				t.Fatalf("job = %+v, want failed safe message containing %q", final, test.wantMessage)
			}
			for _, secret := range []string{marker, password, token, original.Host, "root", fingerprint, "public-cert", "private-key-secret", "trusted-binary", "sealed-token"} {
				if strings.Contains(final.Message, secret) {
					t.Errorf("job message contains private value %q: %s", secret, final.Message)
				}
			}
		})
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
