package provision

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

// fakeExitError mimics the exit status surface of *ssh.ExitError.
type fakeExitError struct{ status int }

func (e fakeExitError) Error() string {
	return fmt.Sprintf("Process exited with status %d: leaked detail", e.status)
}
func (e fakeExitError) ExitStatus() int { return e.status }

func installWithConn(t *testing.T, conn *fakeAgentInstallConn) error {
	t.Helper()
	return RunAgentInstall(context.Background(), validAgentInstallRequest(t), func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
		return conn, nil
	})
}

func TestAgentInstallPreflightExitCodesBecomeTypedFailures(t *testing.T) {
	cases := map[int]string{
		21: AgentInstallCodeNotRoot,
		22: AgentInstallCodeUnsupportedOS,
		23: AgentInstallCodeUnsupportedArch,
		24: AgentInstallCodeUnsupportedArch,
		25: AgentInstallCodeAgentFilesPresent,
		26: AgentInstallCodeSystemdRequired,
		27: AgentInstallCodeAgentInstalled,
	}
	for status, code := range cases {
		conn := &fakeAgentInstallConn{runErrAt: "openvms_install_preflight", runErr: fakeExitError{status}}
		err := installWithConn(t, conn)
		var failure *AgentInstallFailure
		if !errors.As(err, &failure) {
			t.Fatalf("status %d: error %v is not an AgentInstallFailure", status, err)
		}
		if failure.Code != code || failure.ExitStatus != status || failure.Stage != "preflight" {
			t.Fatalf("status %d: failure = %#v, want code %s", status, failure, code)
		}
		if strings.Contains(err.Error(), "leaked") || conn.writes != 0 {
			t.Fatalf("status %d: error leaked remote text or wrote files: %v", status, err)
		}
	}
}

func TestAgentInstallTransferAndActivationFailuresAreTyped(t *testing.T) {
	var failure *AgentInstallFailure
	if err := installWithConn(t, &fakeAgentInstallConn{writeErr: errors.New("sftp secret")}); !errors.As(err, &failure) || failure.Code != AgentInstallCodeTransferFailed || failure.Stage != "transferring" {
		t.Fatalf("transfer failure = %v / %#v", err, failure)
	}
	if err := installWithConn(t, &fakeAgentInstallConn{runErrAt: "systemctl daemon-reload", runErr: fakeExitError{1}}); !errors.As(err, &failure) || failure.Code != AgentInstallCodeActivationFailed || failure.Stage != "activating" {
		t.Fatalf("activation failure = %v / %#v", err, failure)
	}
	if err := installWithConn(t, &fakeAgentInstallConn{runErrAt: "openvms_install_preflight", runErr: errors.New("channel closed")}); !errors.As(err, &failure) || failure.Code != AgentInstallCodePreflightFailed || failure.ExitStatus != 0 {
		t.Fatalf("non-exit preflight failure = %v / %#v", err, failure)
	}
}

func TestAgentInstallFailureMessages(t *testing.T) {
	cases := []struct {
		name     string
		failure  AgentInstallFailure
		bundled  string
		managed  bool
		contains []string
		state    string
	}{
		{"not root", AgentInstallFailure{Code: AgentInstallCodeNotRoot}, "", false, []string{"SSH user is not root"}, ""},
		{"transfer", AgentInstallFailure{Code: AgentInstallCodeTransferFailed}, "", false, []string{"transferred"}, ""},
		{"activation", AgentInstallFailure{Code: AgentInstallCodeActivationFailed}, "", false, []string{"activated"}, ""},
		{"os", AgentInstallFailure{Code: AgentInstallCodeUnsupportedOS}, "", false, []string{"unsupported operating system (Debian or Ubuntu required)"}, ""},
		{"arch", AgentInstallFailure{Code: AgentInstallCodeUnsupportedArch}, "", false, []string{"unsupported or mismatched CPU architecture"}, ""},
		{"files", AgentInstallFailure{Code: AgentInstallCodeAgentFilesPresent}, "", false, []string{"OpenVMS agent files are already present on this host"}, ""},
		{"systemd", AgentInstallFailure{Code: AgentInstallCodeSystemdRequired}, "", false, []string{"systemd is required"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			message, state := agentInstallFailureMessage(&tc.failure, tc.bundled, tc.managed)
			for _, want := range tc.contains {
				if !strings.Contains(message, want) {
					t.Fatalf("message %q lacks %q", message, want)
				}
			}
			if state != tc.state || len(message) > 200 {
				t.Fatalf("state=%q len=%d message=%q", state, len(message), message)
			}
		})
	}
}

func TestRunServerAgentInstallLogsUntypedErrorsWithoutLeakingThem(t *testing.T) {
	var logs bytes.Buffer
	actor, serverID := authz.Actor{UserID: uuid.New()}, uuid.New()
	svc := &Service{
		Log:                            slog.New(slog.NewTextHandler(&logs, nil)),
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		agentInstallExists:             func(context.Context, authz.Actor, uuid.UUID) (bool, error) { return false, nil },
		agentInstallBinary:             func() ([]byte, error) { return []byte("trusted fake binary"), nil },
		agentInstallRun: func(context.Context, AgentInstallRequest, func(string)) error {
			return errors.New("boom with never-persist-ssh-password")
		},
	}
	request := validAgentInstallStartRequest()
	started, err := svc.StartServerAgentInstall(context.Background(), actor, serverID, request)
	if err != nil {
		t.Fatal(err)
	}
	final := waitAgentInstallJob(t, svc, actor, serverID, started)
	if final.Status != "failed" || strings.Contains(final.Message, "boom") {
		t.Fatalf("job = %#v", final)
	}
	if out := logs.String(); !strings.Contains(out, "agent install failed") || strings.Contains(out, request.Password) {
		t.Fatalf("log = %s", out)
	}
}

func TestRunServerAgentInstallPublishesTypedPreflightFailureAndLogsIt(t *testing.T) {
	const password = "never-persist-ssh-password"
	var logs bytes.Buffer
	actor, serverID := authz.Actor{UserID: uuid.New()}, uuid.New()
	svc := &Service{
		Log:                            slog.New(slog.NewTextHandler(&logs, nil)),
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		agentInstallExists:             func(context.Context, authz.Actor, uuid.UUID) (bool, error) { return false, nil },
		agentInstallBinary:             func() ([]byte, error) { return []byte("trusted fake binary"), nil },
		agentInstallRun: func(context.Context, AgentInstallRequest, func(string)) error {
			return &AgentInstallFailure{Code: AgentInstallCodeNotRoot, Stage: "preflight", ExitStatus: 21}
		},
	}
	request := validAgentInstallStartRequest()
	request.Password = password
	started, err := svc.StartServerAgentInstall(context.Background(), actor, serverID, request)
	if err != nil {
		t.Fatal(err)
	}
	final := waitAgentInstallJob(t, svc, actor, serverID, started)
	if final.Status != "failed" || final.Code != AgentInstallCodeNotRoot || final.Message != "SSH user is not root; sign in as root to install the agent" {
		t.Fatalf("job = %#v", final)
	}
	out := logs.String()
	for _, want := range []string{"code=not_root", "stage=preflight", "exit_status=21", "server_id=" + serverID.String(), "host=192.0.2.44"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log lacks %q: %s", want, out)
		}
	}
	if strings.Contains(out, password) {
		t.Fatalf("log leaked the password: %s", out)
	}
}
