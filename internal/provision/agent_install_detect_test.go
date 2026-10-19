package provision

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

func TestAgentInstallPreflightReportsInstalledAgentVersion(t *testing.T) {
	conn := &fakeAgentInstallConn{runErrAt: "openvms_install_preflight", runErr: fakeExitError{27}, runErrOutput: "openvms_install_preflight=agent_present version=0.2.1\n"}
	var failure *AgentInstallFailure
	if err := installWithConn(t, conn); !errors.As(err, &failure) {
		t.Fatalf("error %v is not an AgentInstallFailure", err)
	}
	if failure.Code != AgentInstallCodeAgentInstalled || failure.InstalledVersion != "0.2.1" {
		t.Fatalf("failure = %#v", failure)
	}
	hostile := &fakeAgentInstallConn{runErrAt: "openvms_install_preflight", runErr: fakeExitError{27}, runErrOutput: "openvms_install_preflight=agent_present version=1.0;rm -rf /\n"}
	if err := installWithConn(t, hostile); !errors.As(err, &failure) || failure.InstalledVersion != "" {
		t.Fatalf("hostile version was accepted: %#v", failure)
	}
}

func TestInstalledAgentPreflightKeepsPresenceWhenVersionIsUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
	}{
		{name: "missing", version: ""},
		{name: "unknown", version: "unknown"},
		{name: "malformed", version: "not-a-version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := "openvms_install_preflight=agent_present version=" + tc.version + " os=ubuntu arch=" + runtime.GOARCH + " user=root\n"
			version, present := installedAgentPreflight(output)
			if !present || version != "" {
				t.Fatalf("installedAgentPreflight(%q) = (%q, %t), want unknown version with presence", output, version, present)
			}
		})
	}
}

func TestAgentInstallPreflightScriptReportsVersionReadOnly(t *testing.T) {
	script := agentInstallPreflight()
	for _, want := range []string{"openvms_install_preflight=agent_present version=", "--version", "OPENVMS_AGENT_TOKEN_FILE=/nonexistent", "exit 27"} {
		if !strings.Contains(script, want) {
			t.Fatalf("preflight script lacks %q", want)
		}
	}
}

func TestCompareAgentVersions(t *testing.T) {
	for installed, want := range map[string]string{"0.2.1": AgentVersionOutdated, "0.3.0": AgentVersionUpToDate, "0.10.0": AgentVersionUpToDate, "1.0.0": AgentVersionUpToDate} {
		if got := compareAgentVersions(installed, "0.3.0"); got != want {
			t.Fatalf("compare(%s, 0.3.0) = %s, want %s", installed, got, want)
		}
	}
	for _, bad := range [][2]string{{"", "0.3.0"}, {"dev", "0.3.0"}, {"0.3.0", "dev"}, {"0.3", "0.3.0"}, {"0.3.0-rc1", "0.3.0"}} {
		if got := compareAgentVersions(bad[0], bad[1]); got != AgentVersionUnknown {
			t.Fatalf("compare(%q, %q) = %s, want unknown", bad[0], bad[1], got)
		}
	}
}

func TestRunServerAgentInstallPublishesTypedFailureAndLogsUnderlyingError(t *testing.T) {
	const password = "never-persist-ssh-password"
	var logs bytes.Buffer
	actor, serverID := authz.Actor{UserID: uuid.New()}, uuid.New()
	svc := &Service{
		Log:                            slog.New(slog.NewTextHandler(&logs, nil)),
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		agentInstallBinary:             func() ([]byte, error) { return []byte("trusted fake binary"), nil },
		agentInstallBundledVersion:     func() string { return "0.2.2" },
		agentInstallRun: func(context.Context, AgentInstallRequest, func(string)) error {
			return &AgentInstallFailure{Code: AgentInstallCodeAgentInstalled, Stage: "preflight", ExitStatus: 27, InstalledVersion: "0.2.1"}
		},
	}
	calls := 0
	svc.agentInstallExists = func(context.Context, authz.Actor, uuid.UUID) (bool, error) {
		calls++
		return calls > 1, nil // absent when the install starts, registered by the time it fails
	}
	request := validAgentInstallStartRequest()
	request.Password = password
	started, err := svc.StartServerAgentInstall(context.Background(), actor, serverID, request)
	if err != nil {
		t.Fatal(err)
	}
	final := waitAgentInstallJob(t, svc, actor, serverID, started)
	if final.Status != "failed" || final.Code != AgentInstallCodeAgentInstalled || final.InstalledVersion != "0.2.1" || final.BundledVersion != "0.2.2" || final.VersionState != AgentVersionOutdated || !final.RegisteredHere {
		t.Fatalf("job = %#v", final)
	}
	if !strings.Contains(final.Message, "Actualizar agente") || strings.Contains(final.Message, "remote output was not retained") {
		t.Fatalf("message = %q", final.Message)
	}
	out := logs.String()
	for _, want := range []string{"code=agent_already_installed", "stage=preflight", "exit_status=27", "server_id=" + serverID.String(), "host=192.0.2.44"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log lacks %q: %s", want, out)
		}
	}
	if strings.Contains(out, password) {
		t.Fatalf("log leaked the password: %s", out)
	}
}

func TestAgentInstalledMessages(t *testing.T) {
	cases := []struct {
		name     string
		failure  AgentInstallFailure
		bundled  string
		managed  bool
		contains []string
		state    string
	}{
		{"outdated elsewhere", AgentInstallFailure{Code: AgentInstallCodeAgentInstalled, InstalledVersion: "0.2.1"}, "0.2.2", false,
			[]string{"version 0.2.1 is already installed", "ships 0.2.2", "outdated", "managed by another OpenVMS", "nothing was changed"}, AgentVersionOutdated},
		{"up to date elsewhere", AgentInstallFailure{Code: AgentInstallCodeAgentInstalled, InstalledVersion: "0.3.0"}, "0.3.0", false,
			[]string{"version 0.3.0 is already installed", "up to date"}, AgentVersionUpToDate},
		{"outdated here", AgentInstallFailure{Code: AgentInstallCodeAgentInstalled, InstalledVersion: "0.2.1"}, "0.2.2", true,
			[]string{"outdated", "Actualizar agente"}, AgentVersionOutdated},
		{"unknown installed", AgentInstallFailure{Code: AgentInstallCodeAgentInstalled}, "0.2.2", false,
			[]string{"version unknown", "ships 0.2.2"}, AgentVersionUnknown},
		{"dev bundled", AgentInstallFailure{Code: AgentInstallCodeAgentInstalled, InstalledVersion: "0.2.1"}, "dev", false,
			[]string{"version 0.2.1"}, AgentVersionUnknown},
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
			if tc.state == AgentVersionUnknown && (strings.Contains(message, "outdated") || strings.Contains(message, "up to date")) {
				t.Fatalf("unknown version made a comparison claim: %q", message)
			}
		})
	}
}

func TestAgentInstallPreflightScriptIsValidShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(agentInstallPreflight())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("preflight script has a syntax error: %v: %s", err, out)
	}
}
