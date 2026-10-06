package provision

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"runtime"
	"strings"
	"time"

	agentbundle "github.com/jdolan-exalink/openvms/deploy/agent"
	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/platform/buildinfo"
)

const agentUpdateMaxHealthBytes = 8 << 10

// AgentUpdateRequest contains transient SSH credentials and update payloads.
// It must not be persisted or logged; Go does not guarantee string zeroization.
type AgentUpdateRequest struct {
	Host            string `json:"-"`
	SSHPort         uint16 `json:"-"`
	Username        string `json:"-"`
	Password        string `json:"-"`
	ExpectedHostKey string `json:"-"`
	hostKeyTrust    func(context.Context, string) error
	Binary          []byte `json:"-"`
	AgentToken      string `json:"-"`
	SecurePort      uint16 `json:"-"`
	TLSCertificate  []byte `json:"-"`
	TLSPrivateKey   []byte `json:"-"`
}

func (request AgentUpdateRequest) String() string {
	return fmt.Sprintf("AgentUpdateRequest{host:%q ssh_port:%d user:%q secure_port:%d}", request.Host, request.SSHPort, request.Username, request.SecurePort)
}

func (request AgentUpdateRequest) GoString() string { return request.String() }

func (request AgentUpdateRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Host       string `json:"host"`
		SSHPort    uint16 `json:"ssh_port"`
		Username   string `json:"username"`
		SecurePort uint16 `json:"secure_port"`
	}{request.Host, request.SSHPort, request.Username, request.SecurePort})
}

// AgentUpdateHealthCheck verifies the agent over authenticated HTTPS using its
// registered host, TLS port, bearer and newly installed trust anchor.
type AgentUpdateHealthCheck func(context.Context, string, uint16, string, []byte) error

// RunAgentUpdate safely updates only an existing OpenVMS agent installation.
// It preserves the existing bearer file and refuses unless SSH host-key trust
// confirms a compatible host before any staged file is written.
func RunAgentUpdate(ctx context.Context, request AgentUpdateRequest, dial AgentInstallDialer, checkHealth AgentUpdateHealthCheck) (resultErr error) {
	if err := validateAgentUpdateRequest(request); err != nil {
		return errors.New("invalid agent update request")
	}
	if dial == nil || ctx.Err() != nil {
		return errors.New("agent update unavailable")
	}
	if checkHealth == nil {
		checkHealth = checkAgentUpdateHealth
	}
	updateCtx, cancel := context.WithTimeout(ctx, agentInstallDeadline)
	defer cancel()
	conn, err := dial(updateCtx, request.Host, request.SSHPort, request.Username, request.Password, request.ExpectedHostKey)
	if err != nil {
		return errors.New("agent update SSH connection failed")
	}
	defer conn.Close()
	preflight, err := runAgentInstallStep(updateCtx, conn, agentUpdatePreflight())
	if err != nil || !validAgentUpdatePreflight(preflight) {
		return errors.New("agent update preflight failed")
	}
	unit, err := agentbundle.FS.ReadFile("openvms-agent.service")
	if err != nil {
		return errors.New("agent update service definition unavailable")
	}
	id, err := newAgentUpdateID()
	if err != nil {
		return errors.New("agent update staging unavailable")
	}
	root := "/var/lib/openvms-agent-update/" + id
	retainStage := false
	prepared, err := runAgentInstallStep(updateCtx, conn, agentUpdatePrepare(root, request.SecurePort))
	if err != nil || strings.TrimSpace(prepared) != "openvms_agent_update_staged=ready" {
		return errors.New("agent update staging failed; stage ownership is uncertain and was retained")
	}
	defer func() {
		if !retainStage {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cleanupCancel()
			finalized, err := runAgentInstallStep(cleanupCtx, conn, agentUpdateFinalize(root))
			if (err != nil || strings.TrimSpace(finalized) != "openvms_agent_update_finalized=ok") && resultErr == nil {
				resultErr = errors.New("agent is healthy but secure update staging cleanup failed")
			}
		}
	}()
	files := []agentInstallFile{
		{path: root + "/new/openvms-agent", mode: 0o600, data: request.Binary},
		{path: root + "/new/agent.crt", mode: 0o600, data: request.TLSCertificate},
		{path: root + "/new/agent.key", mode: 0o600, data: request.TLSPrivateKey},
		{path: root + "/new/openvms-agent.service", mode: 0o600, data: unit},
	}
	for _, file := range files {
		if err := updateCtx.Err(); err != nil {
			return errors.New("agent update cancelled before activation")
		}
		writeCtx, writeCancel := context.WithTimeout(updateCtx, 2*time.Minute)
		err = conn.WriteSFTPFile(writeCtx, file.path, file.mode, file.data)
		writeCancel()
		if err != nil {
			return errors.New("agent update staging transfer failed")
		}
	}
	activated, err := runAgentInstallStep(updateCtx, conn, agentUpdateActivate(root))
	if err != nil || strings.TrimSpace(activated) != "openvms_agent_update=active" {
		rollbackErr := rollbackAgentUpdate(conn, root)
		if strings.Contains(rollbackErr.Error(), "uncertain") {
			retainStage = true
		}
		return rollbackErr
	}
	if err := checkHealth(updateCtx, request.Host, request.SecurePort, request.AgentToken, request.TLSCertificate); err != nil {
		rollbackErr := rollbackAgentUpdate(conn, root)
		if strings.Contains(rollbackErr.Error(), "uncertain") {
			retainStage = true
		}
		return rollbackErr
	}
	return nil
}

func validateAgentUpdateRequest(request AgentUpdateRequest) error {
	ip, err := netip.ParseAddr(request.Host)
	validPinnedKey := validFingerprint(request.ExpectedHostKey) && request.hostKeyTrust == nil
	validPersistedTrust := request.ExpectedHostKey == "" && request.hostKeyTrust != nil
	if err != nil || !ip.Is4() || ip.String() != request.Host || request.SSHPort == 0 || request.Username != "root" || request.Password == "" || len(request.Password) > 4096 || (!validPinnedKey && !validPersistedTrust) || len(request.Binary) == 0 || len(request.Binary) > agentInstallMaxBinary || len(request.AgentToken) < 32 || len(request.AgentToken) > 128 || request.SecurePort == 0 || request.SecurePort == 7419 {
		return errInvalidAgentInstall
	}
	for _, char := range request.AgentToken {
		if !(char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
			return errInvalidAgentInstall
		}
	}
	listen := net.JoinHostPort("0.0.0.0", fmt.Sprint(request.SecurePort))
	installRequest := AgentInstallRequest{Host: request.Host, Port: request.SSHPort, User: request.Username, Password: request.Password, ExpectedHostKey: request.ExpectedHostKey, hostKeyTrust: request.hostKeyTrust, Binary: request.Binary, Token: request.AgentToken, TLSListen: listen, TLSCertificate: request.TLSCertificate, TLSPrivateKey: request.TLSPrivateKey}
	if err := validateAgentInstallRequest(installRequest); err != nil {
		return err
	}
	return validateAgentTLSConfig(request.Host, AgentTLSConfig{SecurePort: uint32(request.SecurePort), TrustMode: AgentTLSTrustCustom, CAPEM: request.TLSCertificate})
}

func newAgentUpdateID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func agentUpdatePreflight() string {
	unit, _ := agentbundle.FS.ReadFile("openvms-agent.service")
	unitHash := sha256.Sum256(unit)
	return fmt.Sprintf(`set -eu
[ "$(id -u)" = 0 ] || exit 31
. /etc/os-release
case "${ID:-}" in ubuntu|debian) os_id="$ID" ;; *) exit 32 ;; esac
case "$(uname -m)" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) exit 33 ;; esac
[ "$arch" = %s ] || exit 34
command -v systemctl >/dev/null 2>&1 && command -v sha256sum >/dev/null 2>&1 && command -v install >/dev/null 2>&1 || exit 35
[ ! -L /var/lib/openvms-agent-update ] || exit 45
if [ -e /var/lib/openvms-agent-update ]; then
  [ -d /var/lib/openvms-agent-update ] && [ "$(stat -c '%%u:%%a' /var/lib/openvms-agent-update)" = 0:700 ] || exit 46
fi
systemctl is-active --quiet openvms-agent.service || exit 36
[ "$(systemctl show -p FragmentPath --value openvms-agent.service)" = /etc/systemd/system/openvms-agent.service ] || exit 37
[ -z "$(systemctl show -p DropInPaths --value openvms-agent.service)" ] || exit 38
[ "$(sha256sum /etc/systemd/system/openvms-agent.service | cut -d ' ' -f 1)" = %s ] || exit 39
for target in /usr/local/bin/openvms-agent /etc/openvms/agent.token /etc/openvms/agent.env /etc/systemd/system/openvms-agent.service; do
  [ -f "$target" ] && [ ! -L "$target" ] || exit 40
done
[ "$(stat -c '%%a' /etc/openvms/agent.token)" = 600 ] || exit 41
grep -Fqx 'OPENVMS_AGENT_TOKEN_FILE=/etc/openvms/agent.token' /etc/openvms/agent.env || exit 42
for name in OPENVMS_AGENT_ONVIF_TLS_LISTEN TLS_CERT_FILE TLS_KEY_FILE; do
  [ "$(grep -c "^${name}=" /etc/openvms/agent.env || true)" = 0 ] || exit 43
done
if [ -e /etc/openvms/agent.crt ] || [ -L /etc/openvms/agent.crt ] || [ -e /etc/openvms/agent.key ] || [ -L /etc/openvms/agent.key ]; then
  [ -f /etc/openvms/agent.crt ] && [ ! -L /etc/openvms/agent.crt ] && [ -f /etc/openvms/agent.key ] && [ ! -L /etc/openvms/agent.key ] || exit 44
fi
printf 'openvms_agent_update_preflight=ok os=%%s arch=%%s service=active token=protected tls=disabled unit=compatible\n' "$os_id" "$arch"`, runtime.GOARCH, hex.EncodeToString(unitHash[:]))
}

func validAgentUpdatePreflight(output string) bool {
	fields := strings.Fields(output)
	return len(fields) == 7 && fields[0] == "openvms_agent_update_preflight=ok" && (fields[1] == "os=ubuntu" || fields[1] == "os=debian") && fields[2] == "arch="+runtime.GOARCH && fields[3] == "service=active" && fields[4] == "token=protected" && fields[5] == "tls=disabled" && fields[6] == "unit=compatible"
}

func agentUpdatePrepare(root string, securePort uint16) string {
	return fmt.Sprintf(`set -eu
base=%s
[ ! -e "$base" ] && [ ! -L "$base" ] || exit 51
[ ! -L /var/lib/openvms-agent-update ] || exit 52
if [ -e /var/lib/openvms-agent-update ]; then
  [ -d /var/lib/openvms-agent-update ] && [ "$(stat -c '%%u:%%a' /var/lib/openvms-agent-update)" = 0:700 ] || exit 53
else
  mkdir -m 0700 /var/lib/openvms-agent-update || exit 54
fi
mkdir -m 0700 "$base" || exit 55
mkdir -m 0700 "$base/new" "$base/old" || exit 56
for file in /usr/local/bin/openvms-agent /etc/openvms/agent.env /etc/systemd/system/openvms-agent.service; do
  cp -p "$file" "$base/old/$(basename "$file")"
done
for file in /etc/openvms/agent.crt /etc/openvms/agent.key; do
  if [ -f "$file" ] && [ ! -L "$file" ]; then cp -p "$file" "$base/old/$(basename "$file")"; fi
done
if [ -f /etc/openvms/agent.crt ]; then : > "$base/old/had_tls"; fi
grep -Ev '^(OPENVMS_AGENT_ONVIF_TLS_LISTEN|TLS_CERT_FILE|TLS_KEY_FILE)=' /etc/openvms/agent.env > "$base/new/agent.env"
printf '\nOPENVMS_AGENT_ONVIF_TLS_LISTEN=0.0.0.0:%d\nTLS_CERT_FILE=/etc/openvms/agent.crt\nTLS_KEY_FILE=/etc/openvms/agent.key\n' >> "$base/new/agent.env"
chmod 0600 "$base/new/agent.env"
printf 'openvms_agent_update_staged=ready\n'`, shellWord(root), securePort)
}

func agentUpdateActivate(root string) string {
	return fmt.Sprintf(`set -eu
base=%s
suffix=${base##*/}
temporary_files=
cleanup_temporary_files() {
  for temporary in $temporary_files; do rm -f -- "$temporary"; done
}
trap cleanup_temporary_files EXIT
trap 'exit 1' HUP INT TERM
systemctl stop openvms-agent.service >/dev/null 2>&1
for spec in 'openvms-agent:/usr/local/bin/openvms-agent:0755' 'agent.env:/etc/openvms/agent.env:0600' 'agent.crt:/etc/openvms/agent.crt:0600' 'agent.key:/etc/openvms/agent.key:0600' 'openvms-agent.service:/etc/systemd/system/openvms-agent.service:0644'; do
  name=${spec%%:*}; rest=${spec#*:}; target=${rest%%:*}; mode=${rest##*:}
  temporary=$(mktemp -- "$target.openvms-$suffix.XXXXXX")
  temporary_files="${temporary_files}${temporary_files:+ }$temporary"
  install -o root -g root -m "$mode" "$base/new/$name" "$temporary"
  mv -f "$temporary" "$target"
done
systemctl daemon-reload >/dev/null 2>&1
systemctl restart openvms-agent.service >/dev/null 2>&1
systemctl is-active --quiet openvms-agent.service || exit 61
printf 'openvms_agent_update=active\n'`, shellWord(root))
}

func agentUpdateRollback(root string) string {
	return fmt.Sprintf(`set -eu
base=%s
suffix=${base##*/}
temporary_files=
cleanup_temporary_files() {
  for temporary in $temporary_files; do rm -f -- "$temporary"; done
}
trap cleanup_temporary_files EXIT
trap 'exit 1' HUP INT TERM
systemctl stop openvms-agent.service >/dev/null 2>&1 || exit 71
for pair in 'openvms-agent:/usr/local/bin/openvms-agent' 'agent.env:/etc/openvms/agent.env' 'openvms-agent.service:/etc/systemd/system/openvms-agent.service'; do
  name=${pair%%:*}; target=${pair#*:}
  temporary=$(mktemp -- "$target.openvms-$suffix.restore.XXXXXX")
  temporary_files="${temporary_files}${temporary_files:+ }$temporary"
  cp -p "$base/old/$name" "$temporary"
  mv -f "$temporary" "$target"
done
if [ -f "$base/old/had_tls" ]; then
  for name in agent.crt agent.key; do temporary=$(mktemp -- "/etc/openvms/$name.openvms-$suffix.restore.XXXXXX"); temporary_files="${temporary_files}${temporary_files:+ }$temporary"; cp -p "$base/old/$name" "$temporary"; mv -f "$temporary" "/etc/openvms/$name"; done
else
  rm -f /etc/openvms/agent.crt /etc/openvms/agent.key
fi
systemctl daemon-reload >/dev/null 2>&1
systemctl restart openvms-agent.service >/dev/null 2>&1
systemctl is-active --quiet openvms-agent.service || exit 72
printf 'openvms_agent_update_rollback=ok\n'`, shellWord(root))
}

func agentUpdateFinalize(root string) string {
	return fmt.Sprintf(`set -eu
base=%s
rm -rf -- "$base"
printf 'openvms_agent_update_finalized=ok\n'`, shellWord(root))
}

func rollbackAgentUpdate(conn AgentInstallConn, root string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	output, err := runAgentInstallStep(ctx, conn, agentUpdateRollback(root))
	if err != nil || strings.TrimSpace(output) != "openvms_agent_update_rollback=ok" {
		return errors.New("agent update failed and rollback status is uncertain")
	}
	return errors.New("agent update failed; previous agent files were restored")
}

func checkAgentUpdateHealth(ctx context.Context, host string, securePort uint16, token string, caPEM []byte) error {
	client, err := NewVerifiedAgentHTTPClient(host, securePort, caPEM, false, 5*time.Second)
	if err != nil {
		return errors.New("agent HTTPS health verification failed")
	}
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(ctx, http.MethodGet, "/v1/health", header, nil)
	if err != nil {
		return errors.New("agent HTTPS health verification failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("agent HTTPS health verification failed")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, agentUpdateMaxHealthBytes+1))
	if err != nil || len(body) > agentUpdateMaxHealthBytes {
		return errors.New("agent HTTPS health verification failed")
	}
	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Build   struct {
			Version string `json:"version"`
		} `json:"build"`
	}
	if json.Unmarshal(body, &health) != nil || health.Status != "ok" || health.Version != agent.Version || health.Build.Version != buildinfo.Version {
		return errors.New("agent HTTPS health verification failed")
	}
	return nil
}
