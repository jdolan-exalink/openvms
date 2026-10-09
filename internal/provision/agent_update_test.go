package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/agent"
)

type fakeAgentUpdateConn struct {
	commands   []string
	writes     map[string][]byte
	modes      map[string]os.FileMode
	errAt      string
	writeErr   error
	cleanupErr bool
	preflight  string
}

func (f *fakeAgentUpdateConn) Run(_ context.Context, command string) (string, error) {
	f.commands = append(f.commands, command)
	if f.cleanupErr && strings.Contains(command, "openvms_agent_update_finalized=ok") {
		return "", errors.New("fake cleanup failure")
	}
	if f.errAt != "" && strings.Contains(command, f.errAt) {
		return "remote output containing a secret", errors.New("fake remote failure")
	}
	if strings.Contains(command, "openvms_agent_update_preflight") {
		if f.preflight != "" {
			return f.preflight, nil
		}
		tls := "disabled"
		if strings.Contains(command, "tls_mode=enabled") {
			tls = "enabled"
		}
		return "openvms_agent_update_preflight=ok os=ubuntu arch=" + runtime.GOARCH + " service=active token=protected tls=" + tls + " unit=compatible\n", nil
	}
	if strings.Contains(command, "openvms_agent_update_rollback=ok") {
		return "openvms_agent_update_rollback=ok\n", nil
	}
	if strings.Contains(command, "openvms_agent_update_staged=ready") {
		return "openvms_agent_update_staged=ready\n", nil
	}
	if strings.Contains(command, "openvms_agent_update=active") {
		return "openvms_agent_update=active\n", nil
	}
	if strings.Contains(command, "openvms_agent_update_finalized=ok") {
		return "openvms_agent_update_finalized=ok\n", nil
	}
	return "ok", nil
}

func (f *fakeAgentUpdateConn) WriteSFTPFile(_ context.Context, path string, mode os.FileMode, data []byte) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	if f.writes == nil {
		f.writes = make(map[string][]byte)
		f.modes = make(map[string]os.FileMode)
	}
	f.writes[path] = append([]byte(nil), data...)
	f.modes[path] = mode
	return nil
}

func (f *fakeAgentUpdateConn) Close() error { return nil }

func validAgentUpdateRequest(t *testing.T) AgentUpdateRequest {
	t.Helper()
	cert, key := newAgentInstallTLS(t, "192.0.2.10")
	return AgentUpdateRequest{
		Host: "192.0.2.10", SSHPort: 2202, Username: "root", Password: "update-password-secret",
		ExpectedHostKey: agentInstallTestPin, Binary: []byte("trusted-agent-binary"), AgentToken: "preserved-token-secret-abcdefghijklmnopqrstuvwxyz",
		SecurePort: 7443, TLSCertificate: cert, TLSPrivateKey: key,
	}
}

func TestRunAgentUpdatePreservesIdentityAndVerifiesTLSHealth(t *testing.T) {
	request := validAgentUpdateRequest(t)
	conn := &fakeAgentUpdateConn{}
	var dial []string
	var healthCalls int
	err := RunAgentUpdate(context.Background(), request,
		func(_ context.Context, host string, port uint16, user, password, pin string) (AgentInstallConn, error) {
			dial = []string{host, fmt.Sprint(port), user, password, pin}
			return conn, nil
		}, func(_ context.Context, host string, port uint16, token string, ca []byte) error {
			healthCalls++
			if host != request.Host || port != request.SecurePort || token != request.AgentToken || string(ca) != string(request.TLSCertificate) {
				t.Fatal("health check did not use registered host, preserved token, secure port and new trust")
			}
			return nil
		})
	if err != nil {
		t.Fatalf("RunAgentUpdate: %v", err)
	}
	if got, want := strings.Join(dial, "/"), "192.0.2.10/2202/root/"+request.Password+"/"+request.ExpectedHostKey; got != want {
		t.Fatalf("dial args %q, want %q", got, want)
	}
	if healthCalls != 1 {
		t.Fatalf("health checks = %d, want 1", healthCalls)
	}
	if len(conn.writes) == 0 {
		t.Fatal("update did not stage files")
	}
	commands := strings.Join(conn.commands, "\n")
	for _, forbidden := range []string{request.Password, request.AgentToken, string(request.TLSPrivateKey), "docker", "compose", "apt", "ntp", "/opt/frigate"} {
		if strings.Contains(commands, forbidden) {
			t.Fatalf("SSH command contains secret or out-of-scope operation %q", forbidden)
		}
	}
	for _, required := range []string{"/usr/local/bin/openvms-agent", "/etc/openvms/agent.env", "/etc/openvms/agent.crt", "/etc/openvms/agent.key", "/etc/systemd/system/openvms-agent.service", "openvms_agent_update=active"} {
		if !strings.Contains(commands, required) {
			t.Errorf("update plan omitted allowlisted target %q", required)
		}
	}
	for path, data := range conn.writes {
		if strings.Contains(path, "agent.token") || strings.Contains(string(data), request.AgentToken) {
			t.Fatal("update transfer overwrote or exposed the existing bearer token")
		}
	}
}

func TestRunAgentUpdatePreservesExistingTLSFilesAndTrust(t *testing.T) {
	request := validAgentUpdateRequest(t)
	request.PreserveTLS = true
	request.TLSPrivateKey = nil
	request.TLSMode = AgentTLSTrustCustom
	conn := &fakeAgentUpdateConn{preflight: "openvms_agent_update_preflight=ok os=ubuntu arch=" + runtime.GOARCH + " service=active token=protected tls=enabled unit=compatible\n"}
	err := RunAgentUpdate(context.Background(), request,
		func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
			return conn, nil
		},
		func(_ context.Context, host string, port uint16, token string, ca []byte) error {
			if host != request.Host || port != request.SecurePort || token != request.AgentToken || !bytes.Equal(ca, request.TLSCertificate) {
				t.Fatal("health verification did not use the existing registered trust and bearer")
			}
			return nil
		})
	if err != nil {
		t.Fatalf("preserving TLS update: %v", err)
	}
	for _, path := range []string{"/new/agent.crt", "/new/agent.key", "/new/agent.env"} {
		for written := range conn.writes {
			if strings.HasSuffix(written, path) {
				t.Fatalf("TLS-preserving update wrote protected file %s", written)
			}
		}
	}
	commands := strings.Join(conn.commands, "\n")
	for _, forbidden := range []string{"cp -p /etc/openvms/agent.crt", "cp -p /etc/openvms/agent.key", "cp -p /etc/openvms/agent.env", "agent.crt:/etc/openvms/agent.crt", "agent.key:/etc/openvms/agent.key", "agent.env:/etc/openvms/agent.env"} {
		if strings.Contains(commands, forbidden) {
			t.Fatalf("TLS-preserving update modifies protected file with %q", forbidden)
		}
	}
}

func TestRunAgentUpdateTLSPreservingRollbackNeverReplacesTrustOrEnvironment(t *testing.T) {
	request := validAgentUpdateRequest(t)
	request.PreserveTLS = true
	request.TLSMode = AgentTLSTrustCustom
	request.TLSPrivateKey = nil
	conn := &fakeAgentUpdateConn{}
	err := RunAgentUpdate(context.Background(), request,
		func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
			return conn, nil
		},
		func(context.Context, string, uint16, string, []byte) error { return errors.New("health not verified") })
	var failure *agentUpdateFailure
	if err == nil || !errors.As(err, &failure) || failure.rollback != agentUpdateRollbackRestored {
		t.Fatalf("TLS-preserving health failure = %v, expected confirmed rollback", err)
	}
	commands := strings.Join(conn.commands, "\n")
	for _, forbidden := range []string{"agent.crt:/etc/openvms/agent.crt", "agent.key:/etc/openvms/agent.key", "agent.env:/etc/openvms/agent.env", "cp -p /etc/openvms/agent.key", "rm -f /etc/openvms/agent.crt"} {
		if strings.Contains(commands, forbidden) {
			t.Fatalf("TLS-preserving rollback could change protected file via %q", forbidden)
		}
	}
}

func TestAgentUpdateHealthRequiresExactArtifactDigestAndArchitecture(t *testing.T) {
	expected := agent.BinaryIdentity{SHA256: strings.Repeat("a", 64), Architecture: "amd64"}
	for name, body := range map[string]string{
		"matching artifact":  fmt.Sprintf(`{"status":"ok","binary_identity":{"sha256":"%s","architecture":"amd64"}}`, expected.SHA256),
		"wrong digest":       `{"status":"ok","binary_identity":{"sha256":"` + strings.Repeat("b", 64) + `","architecture":"amd64"}}`,
		"wrong architecture": fmt.Sprintf(`{"status":"ok","binary_identity":{"sha256":"%s","architecture":"arm64"}}`, expected.SHA256),
		"missing identity":   `{"status":"ok"}`,
	} {
		t.Run(name, func(t *testing.T) {
			got := matchesAgentUpdateHealth([]byte(body), expected)
			want := name == "matching artifact"
			if got != want {
				t.Fatalf("matchesAgentUpdateHealth = %v, want %v", got, want)
			}
		})
	}
}

func TestRunAgentUpdateHealthFailureAttemptsRollbackAndNeverClaimsSuccess(t *testing.T) {
	request := validAgentUpdateRequest(t)
	conn := &fakeAgentUpdateConn{}
	healthCalls := 0
	err := RunAgentUpdate(context.Background(), request,
		func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
			return conn, nil
		},
		func(context.Context, string, uint16, string, []byte) error {
			healthCalls++
			return errors.New("untrusted health detail")
		})
	if err == nil || strings.Contains(err.Error(), "untrusted health detail") {
		t.Fatalf("health failure error = %v, want generic failure", err)
	}
	if healthCalls != 1 || !strings.Contains(strings.Join(conn.commands, "\n"), "openvms_agent_update_rollback=ok") {
		t.Fatalf("health failure did not trigger one bounded rollback: calls=%d commands=%q", healthCalls, conn.commands)
	}
}

func TestRunAgentUpdateRejectsInvalidTargetBeforeSSH(t *testing.T) {
	base := validAgentUpdateRequest(t)
	tests := []struct {
		name string
		edit func(*AgentUpdateRequest)
	}{
		{"DNS target", func(r *AgentUpdateRequest) { r.Host = "agent.example.test" }},
		{"missing port", func(r *AgentUpdateRequest) { r.SSHPort = 0 }},
		{"non-root account", func(r *AgentUpdateRequest) { r.Username = "operator" }},
		{"missing pin", func(r *AgentUpdateRequest) { r.ExpectedHostKey = "" }},
		{"missing existing token", func(r *AgentUpdateRequest) { r.AgentToken = "" }},
		{"bad certificate SAN", func(r *AgentUpdateRequest) { r.TLSCertificate, r.TLSPrivateKey = newAgentInstallTLS(t, "192.0.2.11") }},
		{"preserve TLS refuses private key payload", func(r *AgentUpdateRequest) { r.PreserveTLS = true; r.TLSMode = AgentTLSTrustCustom }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := base
			test.edit(&request)
			dials := 0
			err := RunAgentUpdate(context.Background(), request, func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
				dials++
				return &fakeAgentUpdateConn{}, nil
			}, nil)
			if err == nil || dials != 0 || strings.Contains(err.Error(), request.Password) {
				t.Fatalf("invalid update request err=%v dials=%d", err, dials)
			}
		})
	}
}

func TestRunAgentUpdateRejectsIncompatiblePreflightBeforeWriting(t *testing.T) {
	request := validAgentUpdateRequest(t)
	conn := &fakeAgentUpdateConn{preflight: "openvms_agent_update_preflight=ok os=ubuntu arch=wrong service=active token=protected tls=disabled unit=compatible\n"}
	err := RunAgentUpdate(context.Background(), request,
		func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
			return conn, nil
		},
		func(context.Context, string, uint16, string, []byte) error {
			t.Fatal("health called after incompatible host rejection")
			return nil
		})
	if err == nil || len(conn.writes) != 0 {
		t.Fatalf("incompatible preflight err=%v writes=%d", err, len(conn.writes))
	}
}

func TestAgentUpdateRequestFormattingDoesNotExposeCredentialsOrPayload(t *testing.T) {
	request := validAgentUpdateRequest(t)
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, representation := range []string{fmt.Sprint(request), fmt.Sprintf("%+v", request), fmt.Sprintf("%#v", request), string(encoded)} {
		for _, secret := range []string{request.Password, request.AgentToken, request.ExpectedHostKey, string(request.TLSPrivateKey), string(request.TLSCertificate), string(request.Binary)} {
			if strings.Contains(representation, secret) {
				t.Fatalf("agent update serialization leaked secret or payload: %q", representation)
			}
		}
	}
}

func TestRunAgentUpdateActivationFailureRollsBackWithoutHealthSuccess(t *testing.T) {
	request := validAgentUpdateRequest(t)
	conn := &fakeAgentUpdateConn{errAt: "openvms_agent_update=active"}
	var healthCalls int
	err := RunAgentUpdate(context.Background(), request,
		func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
			return conn, nil
		},
		func(context.Context, string, uint16, string, []byte) error { healthCalls++; return nil })
	if err == nil || healthCalls != 0 || !strings.Contains(strings.Join(conn.commands, "\n"), "openvms_agent_update_rollback=ok") {
		t.Fatalf("activation failure err=%v health_calls=%d commands=%q", err, healthCalls, conn.commands)
	}
}

func TestRunAgentUpdateKeepsBackupWhenRollbackIsUncertain(t *testing.T) {
	request := validAgentUpdateRequest(t)
	conn := &fakeAgentUpdateConn{errAt: "openvms_agent_update_rollback"}
	err := RunAgentUpdate(context.Background(), request,
		func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
			return conn, nil
		},
		func(context.Context, string, uint16, string, []byte) error { return errors.New("health unavailable") })
	var failure *agentUpdateFailure
	if err == nil || !errors.As(err, &failure) || failure.rollback != "uncertain" {
		t.Fatalf("rollback uncertainty error = %v", err)
	}
	if strings.Contains(strings.Join(conn.commands, "\n"), "openvms_agent_update_finalized") {
		t.Fatal("rollback evidence was cleaned up despite uncertain restoration")
	}
}

func TestRunAgentUpdateTransferFailureCleansUnactivatedStage(t *testing.T) {
	request := validAgentUpdateRequest(t)
	conn := &fakeAgentUpdateConn{writeErr: errors.New("private transfer error")}
	err := RunAgentUpdate(context.Background(), request,
		func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
			return conn, nil
		},
		func(context.Context, string, uint16, string, []byte) error {
			t.Fatal("health called after transfer failure")
			return nil
		})
	if err == nil || strings.Contains(err.Error(), "private transfer error") {
		t.Fatalf("transfer failure error = %v", err)
	}
	commands := strings.Join(conn.commands, "\n")
	if !strings.Contains(commands, "openvms_agent_update_finalized") || strings.Contains(commands, "openvms_agent_update=active") {
		t.Fatalf("unactivated staging cleanup/activation commands incorrect: %q", commands)
	}
}

func TestRunAgentUpdateDoesNotCleanupUnclaimedOrAmbiguousStage(t *testing.T) {
	request := validAgentUpdateRequest(t)
	conn := &fakeAgentUpdateConn{errAt: "openvms_agent_update_staged=ready"}
	err := RunAgentUpdate(context.Background(), request,
		func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
			return conn, nil
		},
		nil)
	if strings.Contains(strings.Join(conn.commands, "\n"), "openvms_agent_update_finalized=ok") {
		t.Fatal("cleanup was attempted without proof the generated stage path was claimed")
	}
	var failure *agentUpdateFailure
	if err == nil || !errors.As(err, &failure) || failure.code != agentUpdateCodeStagingUncertain || failure.rollback != agentUpdateRollbackUncertain {
		t.Fatalf("prepare failure = %v, want explicit uncertain staging result", err)
	}
}

func TestRunAgentUpdateReturnsSafeTypedFailureDiagnostics(t *testing.T) {
	const marker = "sensitive-ssh-password remote-stderr 203.0.113.77"
	tests := []struct {
		name         string
		conn         *fakeAgentUpdateConn
		dialErr      error
		healthErr    error
		invalid      bool
		wantStage    string
		wantCode     string
		wantRollback string
	}{
		{name: "invalid request", invalid: true, wantStage: "validating", wantCode: "invalid_request", wantRollback: "not_attempted"},
		{name: "known host key mismatch", dialErr: fmt.Errorf("%s: %w", marker, ErrAgentSSHHostKeyMismatch), wantStage: "connecting", wantCode: "ssh_host_key_mismatch", wantRollback: "not_attempted"},
		{name: "unknown nested ssh error", dialErr: errors.New(marker), wantStage: "connecting", wantCode: "ssh_connection_failed", wantRollback: "not_attempted"},
		{name: "preflight error", conn: &fakeAgentUpdateConn{errAt: "openvms_agent_update_preflight"}, wantStage: "preflight", wantCode: "preflight_failed", wantRollback: "not_attempted"},
		{name: "stage claim uncertainty", conn: &fakeAgentUpdateConn{errAt: "openvms_agent_update_staged=ready"}, wantStage: "staging", wantCode: "staging_uncertain", wantRollback: "uncertain"},
		{name: "file transfer failure", conn: &fakeAgentUpdateConn{writeErr: errors.New(marker)}, wantStage: "transferring", wantCode: "transfer_failed", wantRollback: "not_attempted"},
		{name: "transfer failure remains primary when cleanup also fails", conn: &fakeAgentUpdateConn{writeErr: errors.New(marker), cleanupErr: true}, wantStage: "transferring", wantCode: "transfer_failed", wantRollback: "not_attempted"},
		{name: "activation rollback restored", conn: &fakeAgentUpdateConn{errAt: "openvms_agent_update=active"}, wantStage: "activating", wantCode: "activation_failed", wantRollback: "restored"},
		{name: "health rollback uncertain", conn: &fakeAgentUpdateConn{errAt: "openvms_agent_update_rollback=ok"}, healthErr: errors.New(marker), wantStage: "verifying_health", wantCode: "health_check_failed", wantRollback: "uncertain"},
		{name: "health failure remains primary after restored rollback when cleanup also fails", conn: &fakeAgentUpdateConn{cleanupErr: true}, healthErr: errors.New(marker), wantStage: "verifying_health", wantCode: "health_check_failed", wantRollback: "restored"},
		{name: "healthy but staging cleanup failed", conn: &fakeAgentUpdateConn{errAt: "openvms_agent_update_finalized=ok"}, wantStage: "cleanup", wantCode: "cleanup_failed", wantRollback: "not_attempted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validAgentUpdateRequest(t)
			if test.invalid {
				request.Host = "not-an-ip"
			}
			err := RunAgentUpdate(context.Background(), request, func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
				if test.dialErr != nil {
					return nil, test.dialErr
				}
				return test.conn, nil
			}, func(context.Context, string, uint16, string, []byte) error { return test.healthErr })
			if err == nil {
				t.Fatal("expected update failure")
			}
			var failure *agentUpdateFailure
			if !errors.As(err, &failure) {
				t.Fatalf("failure type = %T, want typed safe failure", err)
			}
			if failure.stage != test.wantStage || failure.code != test.wantCode || failure.rollback != test.wantRollback {
				t.Fatalf("diagnostic = (%q, %q, %q), want (%q, %q, %q)", failure.stage, failure.code, failure.rollback, test.wantStage, test.wantCode, test.wantRollback)
			}
			secrets := []string{marker, request.Password, request.AgentToken, request.ExpectedHostKey, string(request.Binary), string(request.TLSCertificate), string(request.TLSPrivateKey)}
			for _, rendered := range []string{err.Error(), fmt.Sprint(err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
				for _, secret := range secrets {
					if secret != "" && strings.Contains(rendered, secret) {
						t.Fatalf("typed error representation leaked nested error or credentials: %q", rendered)
					}
				}
			}
			encoded, marshalErr := json.Marshal(err)
			for _, secret := range secrets {
				if marshalErr != nil || secret != "" && strings.Contains(string(encoded), secret) {
					t.Fatalf("typed error JSON leaked nested error or credentials: %s (err=%v)", encoded, marshalErr)
				}
			}
		})
	}
}

func TestAgentUpdateActivationAndRollbackTrapOwnedTemporaryFiles(t *testing.T) {
	for name, script := range map[string]string{
		"activation": agentUpdateActivate("/var/lib/openvms-agent-update/0123456789abcdef", false),
		"rollback":   agentUpdateRollback("/var/lib/openvms-agent-update/0123456789abcdef", false),
	} {
		t.Run(name, func(t *testing.T) {
			for _, required := range []string{"mktemp --", "temporary_files", "trap", "rm -f --"} {
				if !strings.Contains(script, required) {
					t.Errorf("%s script missing owned-temp safeguard %q", name, required)
				}
			}
			registration := strings.Index(script, `temporary_files="${temporary_files}${temporary_files:+ }$temporary"`)
			transfer := strings.Index(script, "install -o root")
			if name == "rollback" {
				transfer = strings.Index(script, "cp -p")
			}
			if registration < 0 || transfer < 0 || registration > transfer {
				t.Errorf("%s script transfers into a temporary file before registering trap cleanup", name)
			}
		})
	}
}

func TestAgentUpdatePrepareUsesExclusiveStageClaimBeforeSuccessMarker(t *testing.T) {
	root := "/var/lib/openvms-agent-update/0123456789abcdef"
	script := agentUpdatePrepare(root, 7443, false)
	claim := strings.Index(script, `mkdir -m 0700 "$base"`)
	marker := strings.Index(script, "openvms_agent_update_staged=ready")
	if !strings.Contains(script, `[ ! -e "$base" ] && [ ! -L "$base" ]`) || claim < 0 || marker < 0 || claim > marker {
		t.Fatalf("stage claim is not exclusive and preceding the success marker: %q", script)
	}
	if strings.Contains(script, "rm -rf") {
		t.Fatal("prepare plan must never remove an unclaimed stage path")
	}
}

func TestAgentUpdateExistingTLSPlanUsesOnlyFixedTLSLayout(t *testing.T) {
	preflight := agentUpdatePreflight(true, 7443)
	for _, required := range []string{
		"OPENVMS_AGENT_ONVIF_TLS_LISTEN=0.0.0.0:7443",
		"TLS_CERT_FILE=/etc/openvms/agent.crt",
		"TLS_KEY_FILE=/etc/openvms/agent.key",
		"stat -c '%a' /etc/openvms/agent.crt",
		"stat -c '%a' /etc/openvms/agent.key",
		"tls_mode=enabled",
	} {
		if !strings.Contains(preflight, required) {
			t.Errorf("fixed-layout TLS preflight omitted %q", required)
		}
	}
	if validAgentUpdatePreflight("openvms_agent_update_preflight=ok os=ubuntu arch="+runtime.GOARCH+" service=active token=protected tls=disabled unit=compatible", true) {
		t.Fatal("TLS-preserving mode accepted a TLS-disabled target")
	}
	prepare := agentUpdatePrepare("/var/lib/openvms-agent-update/0123456789abcdef", 7443, true)
	activate := agentUpdateActivate("/var/lib/openvms-agent-update/0123456789abcdef", true)
	rollback := agentUpdateRollback("/var/lib/openvms-agent-update/0123456789abcdef", true)
	for name, script := range map[string]string{"prepare": prepare, "activate": activate, "rollback": rollback} {
		for _, forbidden := range []string{"cp -p /etc/openvms/agent.crt", "cp -p /etc/openvms/agent.key", "agent.crt:/etc/openvms/agent.crt", "agent.key:/etc/openvms/agent.key", "agent.env:/etc/openvms/agent.env", "rm -f /etc/openvms/agent.crt"} {
			if strings.Contains(script, forbidden) {
				t.Errorf("%s plan modifies existing TLS material/config via %q", name, forbidden)
			}
		}
		if strings.Contains(script, "%!") {
			t.Errorf("%s shell plan contains fmt formatting error", name)
		}
	}
}
