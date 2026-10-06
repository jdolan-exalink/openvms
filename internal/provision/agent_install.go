package provision

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	agentbundle "github.com/jdolan-exalink/openvms/deploy/agent"
)

const (
	agentInstallDeadline  = 10 * time.Minute
	agentInstallMaxBinary = 128 << 20
	agentInstallMaxTLSPEM = 64 << 10
)

var errInvalidAgentInstall = errors.New("invalid agent install request")

// AgentInstallRequest holds transient operator-supplied SSH credentials and install payloads.
// Callers must not persist or log this value. Go strings are not guaranteed zeroized.
type AgentInstallRequest struct {
	Host            string `json:"-"`
	Port            uint16 `json:"-"`
	User            string `json:"-"`
	Password        string `json:"-"`
	ExpectedHostKey string `json:"-"`
	Binary          []byte `json:"-"`
	Token           string `json:"-"`
	TLSListen       string `json:"-"`
	TLSCertificate  []byte `json:"-"`
	TLSPrivateKey   []byte `json:"-"`
}

// String deliberately exposes only non-secret connection metadata. This keeps
// fmt logging safe even when a caller formats the request with %+v.
func (request AgentInstallRequest) String() string {
	return fmt.Sprintf("AgentInstallRequest{host:%q port:%d user:%q tls_enabled:%t}", request.Host, request.Port, request.User, len(request.TLSCertificate) != 0 || len(request.TLSPrivateKey) != 0 || request.TLSListen != "")
}

// GoString prevents %#v from bypassing the request's redaction boundary.
func (request AgentInstallRequest) GoString() string { return request.String() }

// MarshalJSON emits only safe metadata; the request is not a durable job DTO.
func (request AgentInstallRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Host       string `json:"host,omitempty"`
		Port       uint16 `json:"port,omitempty"`
		User       string `json:"user,omitempty"`
		TLSEnabled bool   `json:"tls_enabled"`
	}{Host: request.Host, Port: request.Port, User: request.User, TLSEnabled: len(request.TLSCertificate) != 0 || len(request.TLSPrivateKey) != 0 || request.TLSListen != ""})
}

// AgentInstallConn is the bounded SSH surface required by the agent-only installer.
type AgentInstallConn interface {
	Run(context.Context, string) (string, error)
	WriteSFTPFile(context.Context, string, os.FileMode, []byte) error
	Close() error
}

// AgentInstallDialer is injected by the future permission-checked API boundary.
// Its implementation must use the exact IPv4, port, root username, password, and pinned key.
type AgentInstallDialer func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error)

// RunAgentInstall installs only the OpenVMS agent into a verified, previously unprovisioned host.
// It does not create a server record, register the token, or claim probe readiness.
func RunAgentInstall(ctx context.Context, request AgentInstallRequest, dial AgentInstallDialer) error {
	return runAgentInstallWithProgress(ctx, request, dial, nil)
}

type agentInstallFile struct {
	path string
	mode os.FileMode
	data []byte
}

func validateAgentInstallRequest(request AgentInstallRequest) error {
	ip := net.ParseIP(request.Host)
	if ip == nil || ip.To4() == nil || ip.String() != request.Host || request.Port == 0 || request.User != "root" || request.Password == "" || len(request.Password) > 4096 || !validFingerprint(request.ExpectedHostKey) || len(request.Binary) == 0 || len(request.Binary) > agentInstallMaxBinary || len(request.Token) < 32 || len(request.Token) > 128 {
		return errInvalidAgentInstall
	}
	for _, r := range request.Token {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return errInvalidAgentInstall
		}
	}
	if len(request.TLSCertificate) == 0 && len(request.TLSPrivateKey) == 0 && request.TLSListen == "" {
		return nil
	}
	if len(request.TLSCertificate) == 0 || len(request.TLSPrivateKey) == 0 || len(request.TLSCertificate) > agentInstallMaxTLSPEM || len(request.TLSPrivateKey) > agentInstallMaxTLSPEM {
		return errInvalidAgentInstall
	}
	listenHost, listenPort, err := net.SplitHostPort(request.TLSListen)
	if err != nil || (listenHost != "0.0.0.0" && listenHost != request.Host) || listenPort == "" {
		return errInvalidAgentInstall
	}
	port, err := strconv.Atoi(listenPort)
	if err != nil || port == 7419 || port < 1 || port > 65535 {
		return errInvalidAgentInstall
	}
	pair, err := tls.X509KeyPair(request.TLSCertificate, request.TLSPrivateKey)
	if err != nil || len(pair.Certificate) == 0 {
		return errInvalidAgentInstall
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || !leaf.NotBefore.Before(time.Now()) || !leaf.NotAfter.After(time.Now()) {
		return errInvalidAgentInstall
	}
	matchedIP := false
	for _, san := range leaf.IPAddresses {
		if san.Equal(ip) {
			matchedIP = true
			break
		}
	}
	if !matchedIP {
		return errInvalidAgentInstall
	}
	return nil
}

func agentInstallFiles(request AgentInstallRequest) ([]agentInstallFile, error) {
	unit, err := agentbundle.FS.ReadFile("openvms-agent.service")
	if err != nil {
		return nil, err
	}
	env := "OPENVMS_AGENT_LISTEN=0.0.0.0:7419\nOPENVMS_AGENT_TOKEN_FILE=/etc/openvms/agent.token\n"
	files := []agentInstallFile{
		{path: "/usr/local/bin/openvms-agent", mode: 0o755, data: request.Binary},
		{path: "/etc/openvms/agent.token", mode: 0o600, data: []byte(request.Token)},
		{path: "/etc/openvms/agent.env", mode: 0o600, data: []byte(env)},
		{path: "/etc/systemd/system/openvms-agent.service", mode: 0o644, data: unit},
	}
	if request.TLSListen != "" {
		env += "OPENVMS_AGENT_ONVIF_TLS_LISTEN=" + request.TLSListen + "\nTLS_CERT_FILE=/etc/openvms/agent.crt\nTLS_KEY_FILE=/etc/openvms/agent.key\n"
		files[2].data = []byte(env)
		files = append(files,
			agentInstallFile{path: "/etc/openvms/agent.crt", mode: 0o600, data: request.TLSCertificate},
			agentInstallFile{path: "/etc/openvms/agent.key", mode: 0o600, data: request.TLSPrivateKey},
		)
	}
	return files, nil
}

func agentInstallPreflight() string {
	return fmt.Sprintf(`set -eu
[ "$(id -u)" = 0 ] || exit 21
. /etc/os-release
case "${ID:-}" in ubuntu|debian) ;; *) exit 22 ;; esac
command -v systemctl >/dev/null 2>&1 || exit 26
if systemctl cat openvms-agent.service >/dev/null 2>&1; then exit 27; fi
case "$(uname -m)" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) exit 23 ;; esac
[ "$arch" = %s ] || exit 24
for target in /usr/local/bin/openvms-agent /etc/openvms/agent.token /etc/openvms/agent.env /etc/systemd/system/openvms-agent.service /etc/openvms/agent.crt /etc/openvms/agent.key; do
  [ ! -e "$target" ] && [ ! -L "$target" ] || exit 25
done
printf 'openvms_install_preflight=ok os=%%s arch=%%s user=root targets=absent\n' "$ID" "$arch"`, shellWord(runtime.GOARCH))
}

func validAgentInstallPreflight(output string) bool {
	fields := strings.Fields(output)
	if len(fields) != 5 || fields[0] != "openvms_install_preflight=ok" || fields[1] != "os=ubuntu" && fields[1] != "os=debian" || fields[2] != "arch="+runtime.GOARCH || fields[3] != "user=root" || fields[4] != "targets=absent" {
		return false
	}
	return true
}

const agentInstallActivate = `set -eu
systemctl daemon-reload >/dev/null 2>&1
systemctl enable openvms-agent.service >/dev/null 2>&1
systemctl restart openvms-agent.service >/dev/null 2>&1
systemctl is-active --quiet openvms-agent.service >/dev/null 2>&1
printf 'openvms_agent_install=active\n'`

func runAgentInstallStep(ctx context.Context, conn AgentInstallConn, command string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", errors.New("agent install cancelled")
	}
	stepCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	output, err := conn.Run(stepCtx, command)
	if err != nil {
		return "", errors.New("remote agent step failed")
	}
	return output, nil
}

func shellWord(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
