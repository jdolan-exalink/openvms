package provision

import (
	"errors"
	"fmt"
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

func preflightFailure(status int) *AgentInstallFailure {
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
	}
	return failure
}

// agentInstallFailureMessage is the operator-facing job message (at most 200 bytes,
// no remote text) and, for an already-installed agent, how its version compares.
func agentInstallFailureMessage(f *AgentInstallFailure, _ string, _ bool) (string, string) {
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
		return "An OpenVMS agent is already installed on this host; nothing was changed", ""
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
