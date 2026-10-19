package provision

import (
	"context"
	"errors"
	"strings"
	"testing"
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
	for _, required := range []string{"systemctl cat openvms-agent.service", "--version", "OPENVMS_AGENT_TOKEN_FILE=/nonexistent"} {
		if !strings.Contains(conn.command, required) {
			t.Fatalf("probe lacks %q", required)
		}
	}
}
