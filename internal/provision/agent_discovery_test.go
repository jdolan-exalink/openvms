package provision

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

type discoveryConn struct {
	output  string
	err     error
	command string
}

func (c *discoveryConn) Run(_ context.Context, command string) (string, error) {
	c.command = command
	return c.output, c.err
}

func TestParseRemoteAgentStatus(t *testing.T) {
	cases := []struct {
		name, output string
		registered   bool
		state        RemoteAgentState
		version      string
	}{
		{"absent", "openvms_agent_discovery=absent\n", false, RemoteAgentAbsent, ""},
		{"unregistered with version", "openvms_agent_discovery=present version=0.2.1\n", false, RemoteAgentPresentUnregistered, "0.2.1"},
		{"registered here", "openvms_agent_discovery=present version=v1.2.3\n", true, RemoteAgentRegisteredHere, "1.2.3"},
		{"present unknown version", "openvms_agent_discovery=present version=unknown\n", false, RemoteAgentPresentUnregistered, ""},
		{"present malformed version", "openvms_agent_discovery=present version=0.2\n", false, RemoteAgentPresentUnregistered, ""},
		{"reject arbitrary remote output", "password=secret\nopenvms_agent_discovery=present version=1.2.3;id\n", false, RemoteAgentUnknown, ""},
		{"malformed", "garbage", false, RemoteAgentUnknown, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRemoteAgentStatus(tc.output, tc.registered)
			if got.State != tc.state || got.Version != tc.version {
				t.Fatalf("status = %#v", got)
			}
		})
	}
}

func TestDiscoverRemoteAgentTransportFailureDoesNotLeakDetails(t *testing.T) {
	conn := &discoveryConn{err: errors.New("secret remote stderr")}
	status, err := DiscoverRemoteAgent(context.Background(), conn, false)
	if err == nil || status.State != RemoteAgentUnknown || strings.Contains(err.Error(), "secret") {
		t.Fatalf("status=%#v err=%v", status, err)
	}
}

func TestDiscoverRemoteAgentUsesOnlyReadOnlyCommand(t *testing.T) {
	conn := &discoveryConn{output: "openvms_agent_discovery=present version=0.2.1\n"}
	status, err := DiscoverRemoteAgent(context.Background(), conn, false)
	if err != nil || status.State != RemoteAgentPresentUnregistered {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	for _, forbidden := range []string{"WriteFile", "sftp", "mkdir", "chmod", "systemctl start", "systemctl enable", "rm ", "install"} {
		if strings.Contains(conn.command, forbidden) {
			t.Fatalf("probe contains mutating operation %q: %s", forbidden, conn.command)
		}
	}
	for _, required := range []string{"systemctl cat openvms-agent.service", "/usr/local/bin/openvms-agent", "[ -L \"$target\" ]", "--version", "OPENVMS_AGENT_TOKEN_FILE=/nonexistent"} {
		if !strings.Contains(conn.command, required) {
			t.Fatalf("probe lacks %q", required)
		}
	}
}

type discoveryInstallConn struct {
	output  string
	err     error
	command string
	writes  int
}

func (c *discoveryInstallConn) Run(_ context.Context, command string) (string, error) {
	c.command = command
	return c.output, c.err
}
func (c *discoveryInstallConn) WriteSFTPFile(context.Context, string, os.FileMode, []byte) error {
	c.writes++
	return nil
}
func (c *discoveryInstallConn) Close() error { return nil }

func TestInspectServerAgentUsesTransientSSHAndReadOnlyDiscovery(t *testing.T) {
	cases := []struct {
		name           string
		output         string
		dialErr        error
		registeredHere bool
		wantState      RemoteAgentState
		wantVersion    string
		wantErr        bool
	}{
		{name: "present known and registered here", output: "openvms_agent_discovery=present version=0.2.1\n", registeredHere: true, wantState: RemoteAgentRegisteredHere, wantVersion: "0.2.1"},
		{name: "present unknown", output: "openvms_agent_discovery=present version=unknown\n", wantState: RemoteAgentPresentUnregistered},
		{name: "absent", output: "openvms_agent_discovery=absent\n", wantState: RemoteAgentAbsent},
		{name: "connection failure", dialErr: errors.New("private transport detail"), wantState: RemoteAgentUnknown, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := &discoveryInstallConn{output: tc.output}
			svc := &Service{
				requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
				agentInstallExists:             func(context.Context, authz.Actor, uuid.UUID) (bool, error) { return tc.registeredHere, nil },
				agentDiscoveryDial: func(_ context.Context, host string, port uint16, user, password, _ string) (AgentInstallConn, error) {
					if host != "192.0.2.44" || port != 2222 || user != "root" || password != "one-time" {
						t.Fatalf("unexpected transient SSH parameters: host=%q port=%d user=%q", host, port, user)
					}
					return conn, tc.dialErr
				},
			}
			got, err := svc.InspectServerAgent(context.Background(), authz.Actor{UserID: uuid.New()}, uuid.New(), AgentInstallStartRequest{Host: "192.0.2.44", Port: 2222, Password: "one-time"})
			if (err != nil) != tc.wantErr || got.State != tc.wantState || got.Version != tc.wantVersion {
				t.Fatalf("InspectServerAgent() = (%#v, %v)", got, err)
			}
			if err != nil && strings.Contains(err.Error(), "private transport detail") {
				t.Fatalf("transport detail leaked: %v", err)
			}
			if conn.writes != 0 {
				t.Fatalf("inspection performed %d remote writes", conn.writes)
			}
			if tc.dialErr == nil {
				for _, forbidden := range []string{"rm ", "chmod", "systemctl start", "systemctl enable", "install", "mkdir"} {
					if strings.Contains(conn.command, forbidden) {
						t.Fatalf("discovery command contains %q: %s", forbidden, conn.command)
					}
				}
			}
		})
	}
}
