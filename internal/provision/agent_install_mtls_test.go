package provision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

const testEnrollToken = "enroll-token-ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"

func mtlsInstall() *AgentMTLSInstall {
	return &AgentMTLSInstall{
		GRPCAddr:    "vms.example.com:9443",
		APIURL:      "https://vms.example.com",
		EnrollToken: testEnrollToken,
	}
}

func installWith(t *testing.T, m *AgentMTLSInstall) (*fakeAgentInstallConn, AgentInstallRequest, error) {
	t.Helper()
	request := validAgentInstallRequest(t)
	request.MTLS = m
	conn := &fakeAgentInstallConn{}
	err := RunAgentInstall(context.Background(), request, func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
		return conn, nil
	})
	return conn, request, err
}

func TestAgentInstallWritesTheMTLSSettingsAndTokenFile(t *testing.T) {
	m := mtlsInstall()
	m.GRPCCAPEM = []byte("-----BEGIN CERTIFICATE-----\ngrpc\n-----END CERTIFICATE-----\n")
	m.APICAPEM = []byte("-----BEGIN CERTIFICATE-----\napi\n-----END CERTIFICATE-----\n")
	m.ServerName = "vms.example.com"
	conn, request, err := installWith(t, m)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(conn.files["/etc/openvms/agent-enroll.token"]); got != testEnrollToken+"\n" {
		t.Fatalf("enroll token file = %q", got)
	}
	if conn.modes["/etc/openvms/agent-enroll.token"] != 0o600 {
		t.Fatalf("enroll token file mode = %o, want 600", conn.modes["/etc/openvms/agent-enroll.token"])
	}
	env := string(conn.files["/etc/openvms/agent.env"])
	for _, want := range []string{
		"OPENVMS_AGENT_GRPC_ADDR=vms.example.com:9443\n",
		"OPENVMS_ENROLL_TOKEN_FILE=/etc/openvms/agent-enroll.token\n",
		"OPENVMS_API_URL=https://vms.example.com\n",
		"OPENVMS_AGENT_STATE_DIR=/var/lib/openvms-agent\n",
		"OPENVMS_CONTROL_TLS_CA_FILE=/etc/openvms/agent-grpc-ca.pem\n",
		"OPENVMS_CONTROL_TLS_SERVER_NAME=vms.example.com\n",
		"OPENVMS_API_CA_FILE=/etc/openvms/agent-api-ca.pem\n",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("agent environment lacks %q:\n%s", want, env)
		}
	}
	if strings.Contains(env, testEnrollToken) {
		t.Fatal("the enrollment token is in the environment file; only its file path may be")
	}
	if conn.modes["/etc/openvms/agent.env"] != 0o600 {
		t.Fatalf("agent.env mode = %o", conn.modes["/etc/openvms/agent.env"])
	}
	if string(conn.files["/etc/openvms/agent-grpc-ca.pem"]) != string(m.GRPCCAPEM) || string(conn.files["/etc/openvms/agent-api-ca.pem"]) != string(m.APICAPEM) {
		t.Fatal("CA bundles were not transferred")
	}
	// The token never reaches a remote command, a log line or a printed request.
	if strings.Contains(strings.Join(conn.commands, "\n"), testEnrollToken) {
		t.Fatal("the enrollment token appears in a remote command")
	}
	encoded, _ := json.Marshal(request)
	for _, rendered := range []string{request.String(), request.GoString(), fmt.Sprintf("%+v", request), fmt.Sprintf("%#v", request), string(encoded), fmt.Sprintf("%+v", m), fmt.Sprintf("%#v", *m)} {
		if strings.Contains(rendered, testEnrollToken) {
			t.Fatalf("a printed form leaks the enrollment token: %s", rendered)
		}
	}
}

func TestAgentInstallWithoutMTLSIsUnchanged(t *testing.T) {
	conn, _, err := installWith(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	env := string(conn.files["/etc/openvms/agent.env"])
	for _, unwanted := range []string{"GRPC_ADDR", "ENROLL", "OPENVMS_API_URL", "STATE_DIR", "CONTROL_TLS"} {
		if strings.Contains(env, unwanted) {
			t.Fatalf("mTLS setting %q written without mTLS: %s", unwanted, env)
		}
	}
	for path := range conn.files {
		if strings.Contains(path, "enroll") || strings.Contains(path, "-ca.pem") {
			t.Fatalf("mTLS file %s written without mTLS", path)
		}
	}
}

func TestAgentInstallRejectsUnsafeMTLSSettingsBeforeDialing(t *testing.T) {
	cases := map[string]func(*AgentMTLSInstall){
		"plain http API URL":       func(m *AgentMTLSInstall) { m.APIURL = "http://vms.example.com" },
		"API URL with credentials": func(m *AgentMTLSInstall) { m.APIURL = "https://user:pw@vms.example.com" },
		"API URL with newline":     func(m *AgentMTLSInstall) { m.APIURL = "https://vms.example.com\nEVIL=1" },
		"address without port":     func(m *AgentMTLSInstall) { m.GRPCAddr = "vms.example.com" },
		"address with a space":     func(m *AgentMTLSInstall) { m.GRPCAddr = "vms.example.com :9443" },
		"address with newline":     func(m *AgentMTLSInstall) { m.GRPCAddr = "vms.example.com:9443\nEVIL=1" },
		"empty token":              func(m *AgentMTLSInstall) { m.EnrollToken = "" },
		"token with newline":       func(m *AgentMTLSInstall) { m.EnrollToken = testEnrollToken + "\nEVIL=1" },
		"token too short":          func(m *AgentMTLSInstall) { m.EnrollToken = "short" },
		"server name with newline": func(m *AgentMTLSInstall) { m.ServerName = "a\nEVIL=1" },
		"oversized CA":             func(m *AgentMTLSInstall) { m.GRPCCAPEM = make([]byte, 128<<10) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := mtlsInstall()
			mutate(m)
			request := validAgentInstallRequest(t)
			request.MTLS = m
			dials := 0
			err := RunAgentInstall(context.Background(), request, func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
				dials++
				return &fakeAgentInstallConn{}, nil
			})
			if err == nil || dials != 0 {
				t.Fatalf("err = %v, dials = %d; want a refusal before any SSH connection", err, dials)
			}
			if strings.Contains(err.Error(), testEnrollToken) {
				t.Fatal("the error leaks the token")
			}
		})
	}
}

// The server-side flow: with the agent listener configured the install job asks for an
// enrollment token for the server being installed, just before the SSH run.
func TestServerAgentInstallRequestsAnEnrollTokenWhenMTLSIsConfigured(t *testing.T) {
	serverID := uuid.New()
	var tokenFor uuid.UUID
	var seen *AgentMTLSInstall
	svc := &Service{
		AgentMTLS: &AgentMTLS{GRPCAddr: "vms.example.com:9443", APIURL: "https://vms.example.com", ServerName: "vms.example.com", GRPCCAPEM: []byte("ca")},
		NewEnrollToken: func(_ context.Context, _ authz.Actor, id uuid.UUID) (string, error) {
			tokenFor = id
			return testEnrollToken, nil
		},
		agentInstallRun: func(_ context.Context, request AgentInstallRequest, _ func(string)) error {
			seen = request.MTLS
			return nil
		},
		agentInstallRegister: func(context.Context, authz.Actor, uuid.UUID, string, uint16, string, []byte) error { return nil },
		agentInstallJobs:     map[uuid.UUID]*agentInstallJob{}, agentInstallByServer: map[uuid.UUID]uuid.UUID{},
	}
	job := &agentInstallJob{serverID: serverID, snapshot: AgentInstallJob{ID: uuid.New()}}
	svc.runServerAgentInstall(context.Background(), authz.Actor{UserID: uuid.New()}, job, AgentInstallRequest{Host: "192.0.2.10"}, "agent-token", nil)
	if tokenFor != serverID {
		t.Fatalf("enroll token requested for %s, want the installed server %s", tokenFor, serverID)
	}
	if seen == nil || seen.EnrollToken != testEnrollToken || seen.GRPCAddr != "vms.example.com:9443" || seen.APIURL != "https://vms.example.com" || seen.ServerName != "vms.example.com" || string(seen.GRPCCAPEM) != "ca" {
		t.Fatalf("install request MTLS = %+v", seen)
	}
	if snap := job.read(); snap.Status != "succeeded" {
		t.Fatalf("job = %+v", snap)
	}
}

func TestServerAgentInstallWithoutMTLSDoesNotTouchEnrollment(t *testing.T) {
	called := false
	var seen *AgentMTLSInstall = &AgentMTLSInstall{}
	svc := &Service{
		NewEnrollToken: func(context.Context, authz.Actor, uuid.UUID) (string, error) { called = true; return "x", nil },
		agentInstallRun: func(_ context.Context, request AgentInstallRequest, _ func(string)) error {
			seen = request.MTLS
			return nil
		},
		agentInstallRegister: func(context.Context, authz.Actor, uuid.UUID, string, uint16, string, []byte) error { return nil },
		agentInstallJobs:     map[uuid.UUID]*agentInstallJob{}, agentInstallByServer: map[uuid.UUID]uuid.UUID{},
	}
	job := &agentInstallJob{serverID: uuid.New(), snapshot: AgentInstallJob{ID: uuid.New()}}
	svc.runServerAgentInstall(context.Background(), authz.Actor{UserID: uuid.New()}, job, AgentInstallRequest{Host: "192.0.2.10"}, "agent-token", nil)
	if called || seen != nil {
		t.Fatalf("enrollment touched without configuration: token requested=%v, MTLS=%+v", called, seen)
	}
}

func TestServerAgentInstallFailsCleanlyWhenTheEnrollTokenCannotBeCreated(t *testing.T) {
	ran := false
	svc := &Service{
		AgentMTLS: &AgentMTLS{GRPCAddr: "vms.example.com:9443", APIURL: "https://vms.example.com"},
		NewEnrollToken: func(context.Context, authz.Actor, uuid.UUID) (string, error) {
			return "", errors.New("db down: secret detail")
		},
		agentInstallRun:  func(context.Context, AgentInstallRequest, func(string)) error { ran = true; return nil },
		agentInstallJobs: map[uuid.UUID]*agentInstallJob{}, agentInstallByServer: map[uuid.UUID]uuid.UUID{},
	}
	job := &agentInstallJob{serverID: uuid.New(), snapshot: AgentInstallJob{ID: uuid.New()}}
	svc.runServerAgentInstall(context.Background(), authz.Actor{UserID: uuid.New()}, job, AgentInstallRequest{Host: "192.0.2.10"}, "agent-token", nil)
	snap := job.read()
	if ran || snap.Status != "failed" || !strings.Contains(snap.Message, "enrollment token") || strings.Contains(snap.Message, "secret detail") {
		t.Fatalf("ran=%v job=%+v; want a failed job that never touched the host and hides the cause", ran, snap)
	}
}

func TestAgentUninstallScriptRemovesTheMTLSState(t *testing.T) {
	script := agentUninstallScript()
	for _, want := range []string{
		"rm -rf /var/lib/openvms-agent/agent-mtls",
		"rm -f /etc/openvms/agent-enroll.token",
		"rm -rf /etc/openvms",
		"openvms_uninstall=ok",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("uninstall script lacks %q", want)
		}
	}
	if i, j := strings.Index(script, "agent-mtls"), strings.Index(script, "printf 'openvms_uninstall=ok"); i < 0 || j < i {
		t.Error("the mTLS state must be removed before the success marker is printed")
	}
}
