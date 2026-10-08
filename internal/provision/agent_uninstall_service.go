package provision

import (
	"context"
	"encoding/json"
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
}

func (request AgentUninstallStartRequest) String() string {
	return "AgentUninstallStartRequest{ssh_port:" + strconv.Itoa(int(request.SSHPort)) + " password:[redacted]}"
}

func (request AgentUninstallStartRequest) GoString() string { return request.String() }

func (request AgentUninstallStartRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		SSHPort  uint16 `json:"ssh_port"`
		Password string `json:"ssh_password"`
	}{request.SSHPort, "[redacted]"})
}

func validateAgentUninstallStartRequest(in AgentUninstallStartRequest) error {
	if in.SSHPort == 0 || in.Password == "" || len(in.Password) > 4096 {
		return &ValidationError{Msg: "invalid agent uninstall request"}
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

	job.update("running", "connecting", "Connecting to the SSH host")
	conn, err := dialAgentSSHWithHostKeyTrust(ctx, row.Host, in.SSHPort, "root", in.Password, func(ctx context.Context, fingerprint string) error {
		return s.trustAgentSSHHostKey(ctx, actor, row.ServerID, row.Host, in.SSHPort, fingerprint)
	})
	if err != nil {
		slog.Warn("uninstall SSH dial failed", "host", row.Host, "err", err)
		job.update("failed", "failed", "Could not establish SSH connection to host")
		return
	}
	defer conn.Close()

	job.update("running", "stopping", "Stopping and disabling agent service")
	cmd := `set -e
if systemctl is-active --quiet openvms-agent.service 2>/dev/null; then
  systemctl stop openvms-agent.service
fi
if systemctl is-enabled --quiet openvms-agent.service 2>/dev/null; then
  systemctl disable openvms-agent.service
fi
rm -f /etc/systemd/system/openvms-agent.service
systemctl daemon-reload
rm -f /usr/local/bin/openvms-agent
rm -rf /etc/openvms
printf 'openvms_uninstall=ok\n'
`
	out, err := conn.Run(ctx, cmd)
	if err != nil || !strings.Contains(out, "openvms_uninstall=ok") {
		slog.Warn("uninstall remote step failed", "err", err, "out", out)
		job.update("failed", "failed", "Failed to remove agent files on host")
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
			job.update("failed", "failed", "Agent files removed on host but database purge failed")
			return
		}
	}

	job.update("succeeded", "complete", "Agent uninstalled successfully")
}
