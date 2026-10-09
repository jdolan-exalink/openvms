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
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime"
	"strings"
	"time"

	agentbundle "github.com/jdolan-exalink/openvms/deploy/agent"
	"github.com/jdolan-exalink/openvms/internal/agent"
)

const agentUpdateMaxHealthBytes = 8 << 10

const (
	agentUpdateStageValidating      = "validating"
	agentUpdateStageConnecting      = "connecting"
	agentUpdateStagePreflight       = "preflight"
	agentUpdateStageStaging         = "staging"
	agentUpdateStageTransferring    = "transferring"
	agentUpdateStageActivating      = "activating"
	agentUpdateStageVerifyingHealth = "verifying_health"
	agentUpdateStageRegistration    = "registration"
	agentUpdateStageCleanup         = "cleanup"
	agentUpdateStageUnknown         = "unknown"

	agentUpdateCodeInvalidRequest        = "invalid_request"
	agentUpdateCodeUnavailable           = "update_unavailable"
	agentUpdateCodeSSHHostKeyMismatch    = "ssh_host_key_mismatch"
	agentUpdateCodeSSHConnectionFailed   = "ssh_connection_failed"
	agentUpdateCodePreflightFailed       = "preflight_failed"
	agentUpdateCodeServiceUnavailable    = "service_definition_unavailable"
	agentUpdateCodeStagingUnavailable    = "staging_unavailable"
	agentUpdateCodeStagingUncertain      = "staging_uncertain"
	agentUpdateCodeCancelled             = "cancelled"
	agentUpdateCodeTransferFailed        = "transfer_failed"
	agentUpdateCodeActivationFailed      = "activation_failed"
	agentUpdateCodeHealthCheckFailed     = "health_check_failed"
	agentUpdateCodeCleanupFailed         = "cleanup_failed"
	agentUpdateCodeTLSRegistrationFailed = "tls_trust_registration_failed"
	agentUpdateCodeUnknown               = "update_failed"

	agentUpdateRollbackNotAttempted = "not_attempted"
	agentUpdateRollbackRestored     = "restored"
	agentUpdateRollbackUncertain    = "uncertain"
	agentUpdateRollbackUnknown      = "unknown"
)

// agentUpdateFailure carries only fixed diagnostic enums. It intentionally does
// not retain, unwrap, or stringify the underlying SSH/remote error.
type agentUpdateFailure struct {
	stage    string
	code     string
	rollback string
}

func newAgentUpdateFailure(stage, code, rollback string) *agentUpdateFailure {
	return &agentUpdateFailure{stage: stage, code: code, rollback: rollback}
}

func (failure *agentUpdateFailure) Error() string { return "agent update failed" }

func (failure *agentUpdateFailure) String() string {
	if failure == nil {
		return "agentUpdateFailure{stage:unknown code:update_failed rollback:unknown}"
	}
	return fmt.Sprintf("agentUpdateFailure{stage:%s code:%s rollback:%s}", failure.stage, failure.code, failure.rollback)
}

func (failure *agentUpdateFailure) GoString() string { return failure.String() }

func (failure *agentUpdateFailure) MarshalJSON() ([]byte, error) {
	if failure == nil {
		return json.Marshal(struct {
			Stage    string `json:"stage"`
			Code     string `json:"code"`
			Rollback string `json:"rollback"`
		}{agentUpdateStageUnknown, agentUpdateCodeUnknown, agentUpdateRollbackUnknown})
	}
	return json.Marshal(struct {
		Stage    string `json:"stage"`
		Code     string `json:"code"`
		Rollback string `json:"rollback"`
	}{failure.stage, failure.code, failure.rollback})
}

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
	TLSMode         string `json:"-"`
	PreserveTLS     bool   `json:"-"`
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
		return newAgentUpdateFailure(agentUpdateStageValidating, agentUpdateCodeInvalidRequest, agentUpdateRollbackNotAttempted)
	}
	if dial == nil || ctx.Err() != nil {
		return newAgentUpdateFailure(agentUpdateStageConnecting, agentUpdateCodeUnavailable, agentUpdateRollbackNotAttempted)
	}
	updateCtx, cancel := context.WithTimeout(ctx, agentInstallDeadline)
	defer cancel()
	conn, err := dial(updateCtx, request.Host, request.SSHPort, request.Username, request.Password, request.ExpectedHostKey)
	if err != nil {
		slog.Warn("agent update dial failed", "host", request.Host, "port", request.SSHPort, "err", err)
		if errors.Is(err, ErrAgentSSHHostKeyMismatch) {
			return newAgentUpdateFailure(agentUpdateStageConnecting, agentUpdateCodeSSHHostKeyMismatch, agentUpdateRollbackNotAttempted)
		}
		return newAgentUpdateFailure(agentUpdateStageConnecting, agentUpdateCodeSSHConnectionFailed, agentUpdateRollbackNotAttempted)
	}
	defer conn.Close()
	preflight, err := runAgentInstallStep(updateCtx, conn, agentUpdatePreflight(request.PreserveTLS, request.SecurePort))
	if err != nil || !validAgentUpdatePreflight(preflight, request.PreserveTLS) {
		return newAgentUpdateFailure(agentUpdateStagePreflight, agentUpdateCodePreflightFailed, agentUpdateRollbackNotAttempted)
	}
	unit, err := agentbundle.FS.ReadFile("openvms-agent.service")
	if err != nil {
		return newAgentUpdateFailure(agentUpdateStagePreflight, agentUpdateCodeServiceUnavailable, agentUpdateRollbackNotAttempted)
	}
	id, err := newAgentUpdateID()
	if err != nil {
		return newAgentUpdateFailure(agentUpdateStageStaging, agentUpdateCodeStagingUnavailable, agentUpdateRollbackNotAttempted)
	}
	root := "/var/lib/openvms-agent-update/" + id
	retainStage := false
	prepared, err := runAgentInstallStep(updateCtx, conn, agentUpdatePrepare(root, request.SecurePort, request.PreserveTLS))
	if err != nil || strings.TrimSpace(prepared) != "openvms_agent_update_staged=ready" {
		return newAgentUpdateFailure(agentUpdateStageStaging, agentUpdateCodeStagingUncertain, agentUpdateRollbackUncertain)
	}
	defer func() {
		if !retainStage {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cleanupCancel()
			finalized, err := runAgentInstallStep(cleanupCtx, conn, agentUpdateFinalize(root))
			if err != nil || strings.TrimSpace(finalized) != "openvms_agent_update_finalized=ok" {
				if resultErr == nil {
					resultErr = newAgentUpdateFailure(agentUpdateStageCleanup, agentUpdateCodeCleanupFailed, agentUpdateRollbackNotAttempted)
				}
			}
		}
	}()
	files := []agentInstallFile{{path: root + "/new/openvms-agent", mode: 0o600, data: request.Binary}}
	if !request.PreserveTLS {
		files = append(files,
			agentInstallFile{path: root + "/new/agent.crt", mode: 0o600, data: request.TLSCertificate},
			agentInstallFile{path: root + "/new/agent.key", mode: 0o600, data: request.TLSPrivateKey},
		)
	}
	files = append(files, agentInstallFile{path: root + "/new/openvms-agent.service", mode: 0o600, data: unit})
	for _, file := range files {
		if err := updateCtx.Err(); err != nil {
			return newAgentUpdateFailure(agentUpdateStageTransferring, agentUpdateCodeCancelled, agentUpdateRollbackNotAttempted)
		}
		writeCtx, writeCancel := context.WithTimeout(updateCtx, 2*time.Minute)
		err = conn.WriteSFTPFile(writeCtx, file.path, file.mode, file.data)
		writeCancel()
		if err != nil {
			return newAgentUpdateFailure(agentUpdateStageTransferring, agentUpdateCodeTransferFailed, agentUpdateRollbackNotAttempted)
		}
	}
	activated, err := runAgentInstallStep(updateCtx, conn, agentUpdateActivate(root, request.PreserveTLS))
	if err != nil || strings.TrimSpace(activated) != "openvms_agent_update=active" {
		slog.Warn("agent update activate failed", "err", err, "output", activated)
		rollback := rollbackAgentUpdate(conn, root, request.PreserveTLS)
		if rollback == agentUpdateRollbackUncertain {
			retainStage = true
		}
		return newAgentUpdateFailure(agentUpdateStageActivating, agentUpdateCodeActivationFailed, rollback)
	}
	if checkHealth == nil {
		identity, identityErr := agent.IdentityFromBinary(request.Binary)
		if identityErr != nil {
			rollback := rollbackAgentUpdate(conn, root, request.PreserveTLS)
			return newAgentUpdateFailure(agentUpdateStageVerifyingHealth, agentUpdateCodeHealthCheckFailed, rollback)
		}
		checkHealth = func(ctx context.Context, host string, port uint16, token string, ca []byte) error {
			return checkAgentUpdateHealth(ctx, host, port, token, ca, request.TLSMode, identity)
		}
	}
	if err := checkHealth(updateCtx, request.Host, request.SecurePort, request.AgentToken, request.TLSCertificate); err != nil {
		rollback := rollbackAgentUpdate(conn, root, request.PreserveTLS)
		if rollback == agentUpdateRollbackUncertain {
			retainStage = true
		}
		return newAgentUpdateFailure(agentUpdateStageVerifyingHealth, agentUpdateCodeHealthCheckFailed, rollback)
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
	installRequest := AgentInstallRequest{Host: request.Host, Port: request.SSHPort, User: request.Username, Password: request.Password, ExpectedHostKey: request.ExpectedHostKey, hostKeyTrust: request.hostKeyTrust, Binary: request.Binary, Token: request.AgentToken}
	if !request.PreserveTLS {
		installRequest.TLSListen, installRequest.TLSCertificate, installRequest.TLSPrivateKey = listen, request.TLSCertificate, request.TLSPrivateKey
	}
	if err := validateAgentInstallRequest(installRequest); err != nil {
		return err
	}
	if request.PreserveTLS {
		if request.TLSPrivateKey != nil || (request.TLSMode != AgentTLSTrustCustom && request.TLSMode != AgentTLSTrustSystem) {
			return errInvalidAgentInstall
		}
		return validateAgentTLSConfig(request.Host, AgentTLSConfig{SecurePort: uint32(request.SecurePort), TrustMode: request.TLSMode, CAPEM: request.TLSCertificate})
	}
	if request.TLSMode != "" && request.TLSMode != AgentTLSTrustCustom {
		return errInvalidAgentInstall
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

func agentUpdatePreflight(preserveTLS bool, securePort uint16) string {
	unit, _ := agentbundle.FS.ReadFile("openvms-agent.service")
	unitHash := sha256.Sum256(unit)
	tlsCheck := `for name in OPENVMS_AGENT_ONVIF_TLS_LISTEN TLS_CERT_FILE TLS_KEY_FILE; do
  [ "$(grep -c "^${name}=" /etc/openvms/agent.env || true)" = 0 ] || exit 43
done
if [ -e /etc/openvms/agent.crt ] || [ -L /etc/openvms/agent.crt ] || [ -e /etc/openvms/agent.key ] || [ -L /etc/openvms/agent.key ]; then
  [ -f /etc/openvms/agent.crt ] && [ ! -L /etc/openvms/agent.crt ] && [ -f /etc/openvms/agent.key ] && [ ! -L /etc/openvms/agent.key ] || exit 44
fi
tls_mode=disabled`
	if preserveTLS {
		tlsCheck = fmt.Sprintf(`grep -Fqx 'OPENVMS_AGENT_ONVIF_TLS_LISTEN=0.0.0.0:%d' /etc/openvms/agent.env || exit 43
grep -Fqx 'TLS_CERT_FILE=/etc/openvms/agent.crt' /etc/openvms/agent.env || exit 43
grep -Fqx 'TLS_KEY_FILE=/etc/openvms/agent.key' /etc/openvms/agent.env || exit 43
[ "$(grep -c '^OPENVMS_AGENT_ONVIF_TLS_LISTEN=' /etc/openvms/agent.env)" = 1 ] || exit 43
[ "$(grep -c '^TLS_CERT_FILE=' /etc/openvms/agent.env)" = 1 ] || exit 43
[ "$(grep -c '^TLS_KEY_FILE=' /etc/openvms/agent.env)" = 1 ] || exit 43
[ -f /etc/openvms/agent.crt ] && [ ! -L /etc/openvms/agent.crt ] && [ "$(stat -c '%%a' /etc/openvms/agent.crt)" = 600 ] || exit 44
[ -f /etc/openvms/agent.key ] && [ ! -L /etc/openvms/agent.key ] && [ "$(stat -c '%%a' /etc/openvms/agent.key)" = 600 ] || exit 44
tls_mode=enabled`, securePort)
	}
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
%s
printf 'openvms_agent_update_preflight=ok os=%%s arch=%%s service=active token=protected tls=%%s unit=compatible\n' "$os_id" "$arch" "$tls_mode"`, runtime.GOARCH, hex.EncodeToString(unitHash[:]), tlsCheck)
}

func validAgentUpdatePreflight(output string, preserveTLS bool) bool {
	fields := strings.Fields(output)
	wantTLS := "tls=disabled"
	if preserveTLS {
		wantTLS = "tls=enabled"
	}
	return len(fields) == 7 && fields[0] == "openvms_agent_update_preflight=ok" && (fields[1] == "os=ubuntu" || fields[1] == "os=debian") && fields[2] == "arch="+runtime.GOARCH && fields[3] == "service=active" && fields[4] == "token=protected" && fields[5] == wantTLS && fields[6] == "unit=compatible"
}

func agentUpdatePrepare(root string, securePort uint16, preserveTLS bool) string {
	backups := `for file in /usr/local/bin/openvms-agent /etc/openvms/agent.env /etc/systemd/system/openvms-agent.service; do
  cp -p "$file" "$base/old/$(basename "$file")"
done
for file in /etc/openvms/agent.crt /etc/openvms/agent.key; do
  if [ -f "$file" ] && [ ! -L "$file" ]; then cp -p "$file" "$base/old/$(basename "$file")"; fi
done
if [ -f /etc/openvms/agent.crt ]; then : > "$base/old/had_tls"; fi`
	newEnv := fmt.Sprintf(`grep -Ev '^(OPENVMS_AGENT_ONVIF_TLS_LISTEN|TLS_CERT_FILE|TLS_KEY_FILE)=' /etc/openvms/agent.env > "$base/new/agent.env"
printf '\nOPENVMS_AGENT_ONVIF_TLS_LISTEN=0.0.0.0:%d\nTLS_CERT_FILE=/etc/openvms/agent.crt\nTLS_KEY_FILE=/etc/openvms/agent.key\n' >> "$base/new/agent.env"
chmod 0600 "$base/new/agent.env"`, securePort)
	if preserveTLS {
		backups = `for file in /usr/local/bin/openvms-agent /etc/systemd/system/openvms-agent.service; do
  cp -p "$file" "$base/old/$(basename "$file")"
done`
		newEnv = `:`
	}
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
%s
%s
printf 'openvms_agent_update_staged=ready\n'`, shellWord(root), backups, newEnv)
}

func agentUpdateActivate(root string, preserveTLS bool) string {
	files := `'openvms-agent:/usr/local/bin/openvms-agent:0755' 'agent.env:/etc/openvms/agent.env:0600' 'agent.crt:/etc/openvms/agent.crt:0600' 'agent.key:/etc/openvms/agent.key:0600' 'openvms-agent.service:/etc/systemd/system/openvms-agent.service:0644'`
	if preserveTLS {
		files = `'openvms-agent:/usr/local/bin/openvms-agent:0755' 'openvms-agent.service:/etc/systemd/system/openvms-agent.service:0644'`
	}
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
for spec in %s; do
  name=${spec%%%%:*}; rest=${spec#*:}; target=${rest%%%%:*}; mode=${rest##*:}
  temporary=$(mktemp -- "$target.openvms-$suffix.XXXXXX")
  temporary_files="${temporary_files}${temporary_files:+ }$temporary"
  install -o root -g root -m "$mode" "$base/new/$name" "$temporary"
  mv -f "$temporary" "$target"
done
systemctl daemon-reload >/dev/null 2>&1
systemctl restart openvms-agent.service >/dev/null 2>&1
systemctl is-active --quiet openvms-agent.service || exit 61
printf 'openvms_agent_update=active\n'`, shellWord(root), files)
}

func agentUpdateRollback(root string, preserveTLS bool) string {
	restore := `for pair in 'openvms-agent:/usr/local/bin/openvms-agent' 'agent.env:/etc/openvms/agent.env' 'openvms-agent.service:/etc/systemd/system/openvms-agent.service'; do
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
fi`
	if preserveTLS {
		restore = `for pair in 'openvms-agent:/usr/local/bin/openvms-agent' 'openvms-agent.service:/etc/systemd/system/openvms-agent.service'; do
  name=${pair%%:*}; target=${pair#*:}
  temporary=$(mktemp -- "$target.openvms-$suffix.restore.XXXXXX")
  temporary_files="${temporary_files}${temporary_files:+ }$temporary"
  cp -p "$base/old/$name" "$temporary"
  mv -f "$temporary" "$target"
done`
	}
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
%s
systemctl daemon-reload >/dev/null 2>&1
systemctl restart openvms-agent.service >/dev/null 2>&1
systemctl is-active --quiet openvms-agent.service || exit 72
printf 'openvms_agent_update_rollback=ok\n'`, shellWord(root), restore)
}

func agentUpdateFinalize(root string) string {
	return fmt.Sprintf(`set -eu
base=%s
rm -rf -- "$base"
printf 'openvms_agent_update_finalized=ok\n'`, shellWord(root))
}

func rollbackAgentUpdate(conn AgentInstallConn, root string, preserveTLS bool) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	output, err := runAgentInstallStep(ctx, conn, agentUpdateRollback(root, preserveTLS))
	if err != nil || strings.TrimSpace(output) != "openvms_agent_update_rollback=ok" {
		return agentUpdateRollbackUncertain
	}
	return agentUpdateRollbackRestored
}

func checkRegisteredAgentUpdateHealth(ctx context.Context, host string, securePort uint16, token string, config AgentTLSConfig) error {
	if uint16(config.SecurePort) != securePort || validateAgentTLSConfig(host, config) != nil {
		return errors.New("registered agent HTTPS health verification failed")
	}
	useSystemRoots := config.TrustMode == AgentTLSTrustSystem
	client, err := NewVerifiedAgentHTTPClient(host, securePort, config.CAPEM, useSystemRoots, 4*time.Second)
	if err != nil {
		return errors.New("registered agent HTTPS health verification failed")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(checkCtx, http.MethodGet, "/v1/health", header, nil)
	if err != nil {
		return errors.New("registered agent HTTPS health verification failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("registered agent HTTPS health verification failed")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, agentUpdateMaxHealthBytes+1))
	if err != nil || len(body) > agentUpdateMaxHealthBytes {
		return errors.New("registered agent HTTPS health verification failed")
	}
	var health struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &health) != nil || health.Status != "ok" {
		return errors.New("registered agent HTTPS health verification failed")
	}
	return nil
}

func checkAgentUpdateHealth(ctx context.Context, host string, securePort uint16, token string, caPEM []byte, trustMode string, expected agent.BinaryIdentity) error {
	useSystemRoots := trustMode == AgentTLSTrustSystem
	if !useSystemRoots && trustMode != AgentTLSTrustCustom {
		return errors.New("agent HTTPS health verification failed")
	}
	client, err := NewVerifiedAgentHTTPClient(host, securePort, caPEM, useSystemRoots, 5*time.Second)
	if err != nil {
		slog.Warn("checkAgentUpdateHealth NewVerifiedAgentHTTPClient failed", "err", err)
		return errors.New("agent HTTPS health verification failed")
	}
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+token)
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for {
		if ctx.Err() != nil {
			return errors.New("agent HTTPS health verification failed")
		}
		response, err := client.Do(ctx, http.MethodGet, "/v1/health", header, nil)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, agentUpdateMaxHealthBytes+1))
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK && readErr == nil && len(body) <= agentUpdateMaxHealthBytes {
				if matchesAgentUpdateHealth(body, expected) {
					return nil
				}
				slog.Warn("checkAgentUpdateHealth matches failed", "body", string(body), "expected_sha", expected.SHA256)
			} else {
				slog.Warn("checkAgentUpdateHealth bad response", "status", response.StatusCode, "readErr", readErr, "token_sha", fmt.Sprintf("%x", sha256.Sum256([]byte(token))))
			}
		} else {
			lastErr = err
		}

		if time.Now().After(deadline) {
			slog.Warn("checkAgentUpdateHealth timed out waiting for agent health", "last_err", lastErr, "host", host, "port", securePort)
			return errors.New("agent HTTPS health verification failed")
		}

		select {
		case <-ctx.Done():
			return errors.New("agent HTTPS health verification failed")
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func matchesAgentUpdateHealth(body []byte, expected agent.BinaryIdentity) bool {
	var health struct {
		Status         string               `json:"status"`
		BinaryIdentity agent.BinaryIdentity `json:"binary_identity"`
	}
	return json.Unmarshal(body, &health) == nil && health.Status == "ok" &&
		validAgentBinaryIdentity(health.BinaryIdentity) &&
		health.BinaryIdentity.SHA256 == expected.SHA256 &&
		health.BinaryIdentity.Architecture == expected.Architecture
}
