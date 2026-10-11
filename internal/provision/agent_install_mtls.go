package provision

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
)

// Paths on the agent host for the mutual-TLS enrollment settings.
const (
	agentEnrollTokenPath = "/etc/openvms/agent-enroll.token"
	agentGRPCCAPath      = "/etc/openvms/agent-grpc-ca.pem"
	agentAPICAPath       = "/etc/openvms/agent-api-ca.pem"
	agentStateDir        = "/var/lib/openvms-agent"
	agentMTLSStateDir    = agentStateDir + "/agent-mtls"
	agentMTLSMaxPEM      = 64 << 10
)

// AgentMTLS is the install-time mutual-TLS configuration of the API, as the agents must see
// it. A nil value on the Service leaves installs exactly as they were. It is set only when the
// agent listener is configured and the operator has said how agents reach the API.
type AgentMTLS struct {
	// GRPCAddr is host:port of the agent listener as reachable from the agent host.
	GRPCAddr string
	// APIURL is the https base URL of the API as reachable from the agent host (enrollment).
	APIURL string
	// ServerName overrides the name checked against the listener's server certificate.
	ServerName string
	// GRPCCAPEM is the PEM bundle that signed the listener's server certificate; empty means
	// the agent uses its system roots. APICAPEM is the same for the API's HTTPS certificate.
	GRPCCAPEM, APICAPEM []byte
}

// AgentMTLSInstall is AgentMTLS plus the one-time enrollment token of one install. The token is
// a secret: it is written only to a 0600 file on the host, never to the environment file, a
// command line or a log, and every printed form of this value omits it.
type AgentMTLSInstall struct {
	GRPCAddr, APIURL, ServerName string
	GRPCCAPEM, APICAPEM          []byte
	EnrollToken                  string
}

func (m AgentMTLSInstall) String() string {
	return fmt.Sprintf("AgentMTLSInstall{grpc:%q api:%q token:[redacted]}", m.GRPCAddr, m.APIURL)
}
func (m AgentMTLSInstall) GoString() string { return m.String() }

// plainValue reports whether s is a non-empty value that is safe on one line of an environment
// file: no whitespace or control characters.
func plainValue(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

func validEnrollToken(t string) bool {
	if len(t) < 20 || len(t) > 256 {
		return false
	}
	for _, r := range t {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// valid checks everything that will be written to the agent host before any SSH connection is
// made, so a bad configuration cannot half-install an agent.
func (m *AgentMTLSInstall) valid() bool {
	if !validEnrollToken(m.EnrollToken) || !plainValue(m.GRPCAddr) || !plainValue(m.APIURL) {
		return false
	}
	host, port, err := net.SplitHostPort(m.GRPCAddr)
	if p, perr := strconv.Atoi(port); err != nil || host == "" || perr != nil || p < 1 || p > 65535 {
		return false
	}
	u, err := url.Parse(m.APIURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if m.ServerName != "" && !plainValue(m.ServerName) {
		return false
	}
	return len(m.GRPCCAPEM) <= agentMTLSMaxPEM && len(m.APICAPEM) <= agentMTLSMaxPEM
}

// apply appends the environment lines and the files of the mTLS settings.
func (m *AgentMTLSInstall) apply(env string, files []agentInstallFile) (string, []agentInstallFile) {
	env += "OPENVMS_AGENT_GRPC_ADDR=" + m.GRPCAddr + "\n" +
		"OPENVMS_ENROLL_TOKEN_FILE=" + agentEnrollTokenPath + "\n" +
		"OPENVMS_API_URL=" + m.APIURL + "\n" +
		"OPENVMS_AGENT_STATE_DIR=" + agentStateDir + "\n"
	files = append(files, agentInstallFile{path: agentEnrollTokenPath, mode: 0o600, data: []byte(m.EnrollToken + "\n")})
	if len(m.GRPCCAPEM) > 0 {
		env += "OPENVMS_CONTROL_TLS_CA_FILE=" + agentGRPCCAPath + "\n"
		files = append(files, agentInstallFile{path: agentGRPCCAPath, mode: 0o644, data: m.GRPCCAPEM})
	}
	if m.ServerName != "" {
		env += "OPENVMS_CONTROL_TLS_SERVER_NAME=" + m.ServerName + "\n"
	}
	if len(m.APICAPEM) > 0 {
		env += "OPENVMS_API_CA_FILE=" + agentAPICAPath + "\n"
		files = append(files, agentInstallFile{path: agentAPICAPath, mode: 0o644, data: m.APICAPEM})
	}
	return env, files
}

// agentUninstallScript stops and removes the agent, including its enrollment token and the
// credentials it enrolled for (key, certificate and CA under the state directory).
func agentUninstallScript() string {
	return `set -e
if systemctl is-active --quiet openvms-agent.service 2>/dev/null; then
  systemctl stop openvms-agent.service
fi
if systemctl is-enabled --quiet openvms-agent.service 2>/dev/null; then
  systemctl disable openvms-agent.service
fi
rm -f /etc/systemd/system/openvms-agent.service
systemctl daemon-reload
rm -f /usr/local/bin/openvms-agent
rm -rf ` + agentMTLSStateDir + `
rmdir ` + agentStateDir + ` 2>/dev/null || true
rm -f ` + agentEnrollTokenPath + `
rm -rf /etc/openvms
printf 'openvms_uninstall=ok\n'
`
}
