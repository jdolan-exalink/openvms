package provision

import (
	"context"
	"errors"
	"strings"
)

var ErrAgentDiscoveryConnection = errors.New("agent discovery connection failed")

// RemoteAgentState describes discovery independently from the local registration record.
type RemoteAgentState string

const (
	RemoteAgentAbsent              RemoteAgentState = "absent"
	RemoteAgentPresentUnregistered RemoteAgentState = "present_unregistered"
	RemoteAgentRegisteredHere      RemoteAgentState = "registered_here"
	RemoteAgentUnknown             RemoteAgentState = "unknown"
)

// RemoteAgentStatus contains only validated, bounded data suitable for callers.
type RemoteAgentStatus struct {
	State   RemoteAgentState
	Version string
}

// AgentDiscoveryConn deliberately excludes file-transfer capabilities: discovery cannot upload.
type AgentDiscoveryConn interface {
	Run(context.Context, string) (string, error)
}

// DiscoverRemoteAgent performs one read-only service/version probe over an existing SSH session.
// It does not establish trust, persist registration, or change the remote host.
func DiscoverRemoteAgent(ctx context.Context, conn AgentDiscoveryConn, registeredHere bool) (RemoteAgentStatus, error) {
	if conn == nil {
		return RemoteAgentStatus{State: RemoteAgentUnknown}, errors.New("agent discovery connection unavailable")
	}
	output, err := conn.Run(ctx, agentDiscoveryProbe())
	if err != nil {
		return RemoteAgentStatus{State: RemoteAgentUnknown}, ErrAgentDiscoveryConnection
	}
	return parseRemoteAgentStatus(output, registeredHere), nil
}

func parseRemoteAgentStatus(output string, registeredHere bool) RemoteAgentStatus {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 1 && fields[0] == "openvms_agent_discovery=absent" {
			return RemoteAgentStatus{State: RemoteAgentAbsent}
		}
		if len(fields) != 2 || fields[0] != "openvms_agent_discovery=present" || !strings.HasPrefix(fields[1], "version=") {
			continue
		}
		version := strings.TrimPrefix(fields[1], "version=")
		if !validAgentDiscoveryVersionToken(version) {
			continue
		}
		if version == "unknown" {
			version = ""
		} else if _, ok := parseAgentSemver(version); ok {
			version = strings.TrimPrefix(version, "v")
		} else {
			version = ""
		}
		state := RemoteAgentPresentUnregistered
		if registeredHere {
			state = RemoteAgentRegisteredHere
		}
		return RemoteAgentStatus{State: state, Version: version}
	}
	return RemoteAgentStatus{State: RemoteAgentUnknown}
}

func validAgentDiscoveryVersionToken(version string) bool {
	if version == "unknown" {
		return true
	}
	for _, r := range version {
		if (r < '0' || r > '9') && r != 'v' && r != '.' {
			return false
		}
	}
	return true
}

func agentDiscoveryProbe() string {
	return `set -eu
if ! command -v systemctl >/dev/null 2>&1; then printf 'openvms_agent_discovery=unknown\n'; exit 0; fi
if ! systemctl cat openvms-agent.service >/dev/null 2>&1; then
  for target in /usr/local/bin/openvms-agent /etc/openvms/agent.token /etc/openvms/agent.env /etc/systemd/system/openvms-agent.service; do
    if [ -e "$target" ] || [ -L "$target" ]; then printf 'openvms_agent_discovery=present version=unknown\n'; exit 0; fi
  done
  printf 'openvms_agent_discovery=absent\n'; exit 0
fi
version=unknown
out=$(OPENVMS_AGENT_TOKEN_FILE=/nonexistent timeout 5 /usr/local/bin/openvms-agent --version 2>/dev/null </dev/null | head -n 1 | tr -d '\r') || out=
case "$out" in ''|*[!v0-9.]*) ;; *) version=$out ;; esac
printf 'openvms_agent_discovery=present version=%s\n' "$version"`
}
