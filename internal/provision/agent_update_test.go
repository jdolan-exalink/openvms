package provision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
)

type fakeAgentUpdateConn struct {
	commands  []string
	writes    map[string][]byte
	modes     map[string]os.FileMode
	errAt     string
	writeErr  error
	preflight string
}

func (f *fakeAgentUpdateConn) Run(_ context.Context, command string) (string, error) {
	f.commands = append(f.commands, command)
	if f.errAt != "" && strings.Contains(command, f.errAt) {
		return "remote output containing a secret", errors.New("fake remote failure")
	}
	if strings.Contains(command, "openvms_agent_update_preflight") {
		if f.preflight != "" {
			return f.preflight, nil
		}
		return "openvms_agent_update_preflight=ok os=ubuntu arch=" + runtime.GOARCH + " service=active token=protected tls=disabled unit=compatible\n", nil
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
	if err == nil || !strings.Contains(err.Error(), "uncertain") {
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
	if err == nil || !strings.Contains(err.Error(), "uncertain") {
		t.Fatalf("prepare failure = %v, want explicit uncertain staging result", err)
	}
}

func TestAgentUpdateActivationAndRollbackTrapOwnedTemporaryFiles(t *testing.T) {
	for name, script := range map[string]string{
		"activation": agentUpdateActivate("/var/lib/openvms-agent-update/0123456789abcdef"),
		"rollback":   agentUpdateRollback("/var/lib/openvms-agent-update/0123456789abcdef"),
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
	script := agentUpdatePrepare(root, 7443)
	claim := strings.Index(script, `mkdir -m 0700 "$base"`)
	marker := strings.Index(script, "openvms_agent_update_staged=ready")
	if !strings.Contains(script, `[ ! -e "$base" ] && [ ! -L "$base" ]`) || claim < 0 || marker < 0 || claim > marker {
		t.Fatalf("stage claim is not exclusive and preceding the success marker: %q", script)
	}
	if strings.Contains(script, "rm -rf") {
		t.Fatal("prepare plan must never remove an unclaimed stage path")
	}
}
