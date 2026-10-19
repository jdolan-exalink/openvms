package provision

import (
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// Machine-readable install failure codes. They are part of the job API contract
// (AgentInstallJob.code) and are localized by the web client.
const (
	AgentInstallCodeNotRoot            = "not_root"
	AgentInstallCodeUnsupportedOS      = "unsupported_os"
	AgentInstallCodeUnsupportedArch    = "unsupported_architecture"
	AgentInstallCodeAgentFilesPresent  = "agent_files_present"
	AgentInstallCodeSystemdRequired    = "systemd_required"
	AgentInstallCodeAgentInstalled     = "agent_already_installed"
	AgentInstallCodePreflightFailed    = "preflight_failed"
	AgentInstallCodeTransferFailed     = "transfer_failed"
	AgentInstallCodeActivationFailed   = "activation_failed"
	AgentInstallCodeRegistrationFailed = "registration_failed"
	AgentInstallCodeSSHAuthFailed      = "ssh_auth_failed"
	AgentInstallCodeSSHConnectFailed   = "ssh_connection_failed"
	agentInstallStagePreflight         = "preflight"
	agentInstallStageConnecting        = "connecting"
	agentInstallStageTransferring      = "transferring"
	agentInstallStageActivating        = "activating"
	agentInstallStageValidatingPayload = "validating"
)

// AgentInstallFailure is the typed, secret-free reason an agent install stopped.
// It carries the remote exit status when the failing step reported one, never
// remote output text.
type AgentInstallFailure struct {
	Code             string
	Stage            string
	ExitStatus       int
	InstalledVersion string
}

func (f *AgentInstallFailure) Error() string {
	switch f.Code {
	case AgentInstallCodeSSHAuthFailed:
		return "agent install SSH authentication failed: invalid password"
	case AgentInstallCodeSSHConnectFailed:
		return "agent install SSH connection failed"
	case AgentInstallCodeTransferFailed:
		return "agent install file transfer failed"
	case AgentInstallCodeActivationFailed:
		return "agent install activation failed"
	case AgentInstallCodeRegistrationFailed:
		return "agent enrollment or health verification failed; registration was not saved"
	default:
		return "agent install preflight failed: " + f.Code
	}
}

// remoteStepError reports that a remote command ran and exited non-zero.
type remoteStepError struct{ status int }

func (e *remoteStepError) Error() string {
	return fmt.Sprintf("remote agent step exited with status %d", e.status)
}

// remoteExitStatus extracts a remote exit status from an SSH run error
// (*ssh.ExitError satisfies this) without keeping any of its text.
func remoteExitStatus(err error) (int, bool) {
	var exit interface{ ExitStatus() int }
	if errors.As(err, &exit) {
		return exit.ExitStatus(), true
	}
	return 0, false
}

func preflightFailure(status int, output string) *AgentInstallFailure {
	failure := &AgentInstallFailure{Stage: agentInstallStagePreflight, ExitStatus: status, Code: AgentInstallCodePreflightFailed}
	switch status {
	case 21:
		failure.Code = AgentInstallCodeNotRoot
	case 22:
		failure.Code = AgentInstallCodeUnsupportedOS
	case 23, 24:
		failure.Code = AgentInstallCodeUnsupportedArch
	case 25:
		failure.Code = AgentInstallCodeAgentFilesPresent
	case 26:
		failure.Code = AgentInstallCodeSystemdRequired
	case 27:
		failure.Code = AgentInstallCodeAgentInstalled
		failure.InstalledVersion = parseAgentPresentVersion(output)
	}
	return failure
}

// agentInstallFailureMessage is the operator-facing job message (at most 200 bytes,
// no remote text) and, for an already-installed agent, how its version compares.
func agentInstallFailureMessage(f *AgentInstallFailure, bundled string, managedHere bool) (string, string) {
	switch f.Code {
	case AgentInstallCodeNotRoot:
		return "SSH user is not root; sign in as root to install the agent", ""
	case AgentInstallCodeUnsupportedOS:
		return "The host has an unsupported operating system (Debian or Ubuntu required); nothing was changed", ""
	case AgentInstallCodeUnsupportedArch:
		return "The host has an unsupported or mismatched CPU architecture for this agent build; nothing was changed", ""
	case AgentInstallCodeAgentFilesPresent:
		return "OpenVMS agent files are already present on this host; nothing was changed", ""
	case AgentInstallCodeSystemdRequired:
		return "systemd is required on the host; nothing was changed", ""
	case AgentInstallCodeAgentInstalled:
		return agentInstalledMessage(f.InstalledVersion, bundled, managedHere)
	case AgentInstallCodePreflightFailed:
		return "The host preflight check failed; nothing was changed", ""
	case AgentInstallCodeTransferFailed:
		return "Agent files could not be transferred; inspect the host before retrying", ""
	case AgentInstallCodeActivationFailed:
		return "The agent service could not be activated; inspect the host before retrying", ""
	case AgentInstallCodeSSHAuthFailed:
		return "SSH authentication failed: invalid password or credentials", ""
	case AgentInstallCodeSSHConnectFailed:
		return "Could not establish SSH. Verify reachability and root SSH access, then inspect the target before retrying.", ""
	}
	return "Agent installation failed; remote output was not retained", ""
}

// How an installed agent version compares with the one this OpenVMS ships.
const (
	AgentVersionUpToDate = "up_to_date"
	AgentVersionOutdated = "outdated"
	AgentVersionUnknown  = "unknown"
)

const agentPresentPreflightPrefix = "openvms_install_preflight=agent_present"

var agentSemver = regexp.MustCompile(`^v?(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)

func parseAgentSemver(version string) ([3]int, bool) {
	match := agentSemver.FindStringSubmatch(version)
	if match == nil {
		return [3]int{}, false
	}
	var parts [3]int
	for i := range parts {
		parts[i], _ = strconv.Atoi(match[i+1])
	}
	return parts, true
}

// parseAgentPresentVersion reads the strict preflight line
// "openvms_install_preflight=agent_present version=X". Anything else, including a
// version that is not MAJOR.MINOR.PATCH, yields "" (unknown); remote text is never kept.
func parseAgentPresentVersion(output string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || len(fields) > 5 || fields[0] != agentPresentPreflightPrefix || !strings.HasPrefix(fields[1], "version=") {
			continue
		}
		version := strings.TrimPrefix(fields[1], "version=")
		if _, ok := parseAgentSemver(version); ok {
			return strings.TrimPrefix(version, "v")
		}
	}
	return ""
}

func installedAgentPreflight(output string) (string, bool) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 || fields[0] != agentPresentPreflightPrefix || !strings.HasPrefix(fields[1], "version=") {
			continue
		}
		version := strings.TrimPrefix(fields[1], "version=")
		if fields[2] != "os=ubuntu" && fields[2] != "os=debian" || fields[3] != "arch="+runtime.GOARCH || fields[4] != "user=root" {
			continue
		}
		// Service presence and host compatibility are independent of whether
		// the installed binary can provide a parseable version.
		if _, ok := parseAgentSemver(version); !ok {
			version = ""
		} else {
			version = strings.TrimPrefix(version, "v")
		}
		return version, true
	}
	return "", false
}

// compareAgentVersions says whether the installed agent is behind the bundled one.
// Only MAJOR.MINOR.PATCH versions are compared; dev, empty or pre-release labels are unknown.
func compareAgentVersions(installed, bundled string) string {
	a, okA := parseAgentSemver(installed)
	b, okB := parseAgentSemver(bundled)
	if !okA || !okB {
		return AgentVersionUnknown
	}
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return AgentVersionOutdated
			}
			return AgentVersionUpToDate
		}
	}
	return AgentVersionUpToDate
}

func compareAgentVersionOrder(installed, bundled string) (int, bool) {
	a, okA := parseAgentSemver(installed)
	b, okB := parseAgentSemver(bundled)
	if !okA || !okB {
		return 0, false
	}
	for i := range a {
		if a[i] < b[i] {
			return -1, true
		}
		if a[i] > b[i] {
			return 1, true
		}
	}
	return 0, true
}

func agentInstalledMessage(installed, bundled string, managedHere bool) (string, string) {
	state := compareAgentVersions(installed, bundled)
	subject := "An OpenVMS agent (version unknown)"
	if installed != "" {
		subject = "An OpenVMS agent version " + installed
	}
	shipped := "bundled version unknown"
	if _, ok := parseAgentSemver(bundled); ok {
		shipped = "this OpenVMS ships " + strings.TrimPrefix(bundled, "v")
	}
	message := subject + " is already installed (" + shipped + ")"
	switch state {
	case AgentVersionUpToDate:
		message += ": up to date"
	case AgentVersionOutdated:
		message += ": outdated"
	}
	if managedHere {
		return message + `. Use "Actualizar agente" to update it.`, state
	}
	return message + ". It is probably managed by another OpenVMS; nothing was changed.", state
}
