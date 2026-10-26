package provision

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// AgentUninstallStartRequest is the request to uninstall a registered agent over SSH.
type AgentUninstallStartRequest struct {
	SSHPort  uint16
	Password string
	// ConfigAction is what to do with Frigate's configuration: "" or keep leaves it as it is,
	// restore_original puts back the copy the agent saved before OpenVMS touched it.
	ConfigAction string
	// DiscardBackup also deletes the agent's saved original from the host.
	DiscardBackup bool
}

// Values of AgentUninstallStartRequest.ConfigAction.
const (
	ConfigActionKeep            = "keep"
	ConfigActionRestoreOriginal = "restore_original"
)

func (request AgentUninstallStartRequest) String() string {
	return "AgentUninstallStartRequest{ssh_port:" + strconv.Itoa(int(request.SSHPort)) + " password:[redacted] config_action:" + request.ConfigAction + " discard_backup:" + strconv.FormatBool(request.DiscardBackup) + "}"
}

func (request AgentUninstallStartRequest) GoString() string { return request.String() }

func (request AgentUninstallStartRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		SSHPort       uint16 `json:"ssh_port"`
		Password      string `json:"ssh_password"`
		ConfigAction  string `json:"config_action"`
		DiscardBackup bool   `json:"discard_backup"`
	}{request.SSHPort, "[redacted]", request.ConfigAction, request.DiscardBackup})
}

func validateAgentUninstallStartRequest(in AgentUninstallStartRequest) error {
	if in.SSHPort == 0 || in.Password == "" || len(in.Password) > 4096 {
		return &ValidationError{Msg: "invalid agent uninstall request"}
	}
	switch in.ConfigAction {
	case "", ConfigActionKeep, ConfigActionRestoreOriginal:
	default:
		return &ValidationError{Msg: "config_action must be keep or restore_original"}
	}
	return nil
}

// StartServerAgentUninstall creates a bounded in-memory uninstall job.
func (s *Service) StartServerAgentUninstall(ctx context.Context, actor authz.Actor, serverID uuid.UUID, in AgentUninstallStartRequest) (AgentInstallJob, error) {
	if serverID == uuid.Nil {
		return AgentInstallJob{}, &ValidationError{Msg: "server is required"}
	}
	if err := validateAgentUninstallStartRequest(in); err != nil {
		return AgentInstallJob{}, err
	}
	if err := s.requireAgentUpdate(ctx, actor, serverID); err != nil {
		return AgentInstallJob{}, err
	}
	job, err := s.reserveAgentInstallJob(actor.UserID, serverID)
	if err != nil {
		return AgentInstallJob{}, err
	}
	reservationOwned := true
	defer func() {
		if reservationOwned {
			s.releaseAgentInstallJob(job, true)
		}
	}()

	load := s.loadAgentUpdateRegistration
	if load == nil {
		load = s.loadRegisteredAgentForUpdate
	}
	row, _, err := load(ctx, actor, serverID)
	if err != nil {
		return AgentInstallJob{}, err
	}
	host := net.ParseIP(row.Host)
	if row.ServerID != serverID || host == nil || host.To4() == nil || host.To4().String() != row.Host {
		return AgentInstallJob{}, ErrAgentUpdateUnavailable
	}

	// #nosec G118 -- durable uninstall job must outlive the HTTP request.
	go s.runServerAgentUninstall(context.Background(), actor, job, row, in)
	reservationOwned = false
	return job.read(), nil
}

func (s *Service) runServerAgentUninstall(parent context.Context, actor authz.Actor, job *agentInstallJob, row db.ServerAgent, in AgentUninstallStartRequest) {
	ctx, cancel := context.WithTimeout(parent, agentUpdateJobTimeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			job.update("failed", "failed", "Agent uninstallation failed unexpectedly")
		}
		in.Password = ""
		s.releaseAgentInstallJob(job, false)
	}()

	restored := false
	if in.ConfigAction == ConfigActionRestoreOriginal {
		if !s.restoreOriginalConfigForUninstall(ctx, actor, job, row.ServerID) {
			return
		}
		restored = true
	}
	// failed ends the job; once the configuration was restored the message must say so.
	failed := func(message string) {
		if restored {
			message += "; the original Frigate configuration was already restored"
		}
		job.update("failed", "failed", message)
	}

	job.update("running", "connecting", "Connecting to the SSH host")
	dial := s.agentUninstallDial
	if dial == nil {
		dial = func(ctx context.Context, host string, port uint16, password string, trust func(context.Context, string) error) (AgentInstallConn, error) {
			return dialAgentSSHWithHostKeyTrust(ctx, host, port, "root", password, trust)
		}
	}
	conn, err := dial(ctx, row.Host, in.SSHPort, in.Password, func(ctx context.Context, fingerprint string) error {
		return s.trustAgentSSHHostKey(ctx, actor, row.ServerID, row.Host, in.SSHPort, fingerprint)
	})
	if err != nil {
		slog.Warn("uninstall SSH dial failed", "host", row.Host, "err", err)
		failed("Could not establish SSH connection to host")
		return
	}
	defer conn.Close()

	job.update("running", "stopping", "Stopping and disabling agent service")
	cmd := agentUninstallScript(in.DiscardBackup)
	out, err := conn.Run(ctx, cmd)
	if err != nil || !strings.Contains(out, "openvms_uninstall=ok") {
		slog.Warn("uninstall remote step failed", "err", err, "out", out)
		failed("Failed to remove agent files on host")
		return
	}

	job.update("running", "cleaning", "Purging agent registration from database")
	if s.Store != nil {
		err = s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
			_, delErr := q.DeleteServerAgent(ctx, row.ServerID)
			return delErr
		})
		if err != nil {
			slog.Warn("uninstall db purge failed", "server_id", row.ServerID, "err", err)
			failed("Agent files removed on host but database purge failed")
			return
		}
	}

	job.update("succeeded", "complete", "Agent uninstalled successfully")
}

// restoreOriginalConfigForUninstall puts the agent's saved original Frigate configuration back, before
// anything is removed. It reports false after failing the job: nothing was uninstalled then.
func (s *Service) restoreOriginalConfigForUninstall(ctx context.Context, actor authz.Actor, job *agentInstallJob, serverID uuid.UUID) bool {
	job.update("running", "restoring", "Restoring the original Frigate configuration")
	read := s.agentUninstallReadOriginal
	if read == nil {
		read = s.readAgentOriginalConfig
	}
	original, found, err := read(ctx, actor, serverID)
	if err != nil {
		slog.Warn("uninstall: the original configuration could not be read", "server_id", serverID, "err", err)
		job.update("failed", "failed", "The original configuration could not be read from the agent; nothing was changed")
		return false
	}
	if !found {
		job.update("failed", "failed", "No original configuration is stored on the agent; nothing was changed")
		return false
	}
	save := s.agentUninstallSaveConfig
	if save == nil {
		save = func(ctx context.Context, actor authz.Actor, serverID uuid.UUID, yaml string) error {
			if s.Inv == nil {
				return errors.New("inventory unavailable")
			}
			_, err := s.Inv.SaveServerFrigateRaw(ctx, actor, serverID, yaml, true)
			return err
		}
	}
	if err := save(ctx, actor, serverID, original); err != nil {
		slog.Warn("uninstall: the original configuration could not be restored", "server_id", serverID, "err", err)
		job.update("failed", "failed", "The original configuration could not be restored; the agent was not uninstalled")
		return false
	}
	return true
}
