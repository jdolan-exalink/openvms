package provision

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const agentUpdateJobTimeout = 10 * time.Minute

type AgentUpdateStartRequest struct {
	SSHPort  uint16
	Password string
}

func (request AgentUpdateStartRequest) String() string {
	return "AgentUpdateStartRequest{ssh_port:" + strconv.Itoa(int(request.SSHPort)) + " password:[redacted]}"
}

func (request AgentUpdateStartRequest) GoString() string { return request.String() }

func (request AgentUpdateStartRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		SSHPort  uint16 `json:"ssh_port"`
		Password string `json:"ssh_password"`
	}{request.SSHPort, "[redacted]"})
}

func validateAgentUpdateStartRequest(in AgentUpdateStartRequest) error {
	if in.SSHPort == 0 || in.Password == "" || len(in.Password) > 4096 {
		return &ValidationError{Msg: "invalid agent update request"}
	}
	return nil
}

var ErrAgentUpdateUnavailable = errors.New("registered agent update unavailable")
var ErrAgentUpdateTLSAlreadyConfigured = errors.New("agent update only supports a TLS-disabled registered agent")

type agentUpdateTxChecker interface {
	Require(authz.Permission, authz.Resource) error
}

type agentUpdateTxQueries interface {
	GetServerForUpdate(context.Context, uuid.UUID) (db.GetServerForUpdateRow, error)
	GetServerAgentForUpdate(context.Context, uuid.UUID) (db.ServerAgent, error)
	RegisterServerAgentTLSIfAbsent(context.Context, db.RegisterServerAgentTLSIfAbsentParams) (int64, error)
	InsertAudit(context.Context, db.InsertAuditParams) error
}

type agentUpdateTxCheckerLoader func(context.Context, db.GetServerForUpdateRow) (agentUpdateTxChecker, error)

// persistAgentUpdateTLSInTransaction is shared by the production Store.Tx path and
// deterministic query fakes. Its reads, authorization, comparison and insert all run
// inside the caller's transaction.
func persistAgentUpdateTLSInTransaction(ctx context.Context, q agentUpdateTxQueries, loadChecker agentUpdateTxCheckerLoader, actor authz.Actor, original db.ServerAgent, config AgentTLSConfig) error {
	server, err := q.GetServerForUpdate(ctx, original.ServerID)
	if err != nil {
		return store.Classify(err)
	}
	if server.ID != original.ServerID || server.TenantID != original.TenantID {
		return ErrAgentUpdateUnavailable
	}
	checker, err := loadChecker(ctx, server)
	if err != nil {
		return err
	}
	resource := access.Server(server.TenantID, server.SiteID, server.ID)
	if err := checker.Require(authz.ServersManage, resource); err != nil {
		return err
	}
	if err := checker.Require(authz.ServersConfigSecrets, resource); err != nil {
		return err
	}
	current, err := q.GetServerAgentForUpdate(ctx, original.ServerID)
	if err != nil {
		return store.Classify(err)
	}
	if !sameAgentUpdateBinding(current, original) || config.SecurePort < 1 || config.SecurePort > 65535 || config.TrustMode != AgentTLSTrustCustom || validateAgentTLSConfig(original.Host, config) != nil {
		return ErrAgentUpdateUnavailable
	}
	rows, err := q.RegisterServerAgentTLSIfAbsent(ctx, db.RegisterServerAgentTLSIfAbsentParams{
		ServerID: original.ServerID, TenantID: original.TenantID, SecurePort: int32(config.SecurePort),
		TrustMode: config.TrustMode, CaPem: nullCA(config),
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrAgentUpdateTLSAlreadyConfigured
	}
	return auditAgentTLS(ctx, q, actor, original.TenantID, original.ServerID, "SERVER_AGENT_UPDATED_TLS_VERIFIED", nil, &config)
}

func sameAgentUpdateBinding(current, original db.ServerAgent) bool {
	return current.ServerID == original.ServerID && current.TenantID == original.TenantID &&
		current.Host == original.Host && current.Port == original.Port && equalBytes(current.TokenSealed, original.TokenSealed)
}

// StartServerAgentUpdate creates a bounded, in-memory update job after checking both
// server permissions. The destination IPv4 and existing bearer come from registration,
// never from the request. Agent TLS trust is persisted only after authenticated HTTPS
// health succeeds.
func (s *Service) StartServerAgentUpdate(ctx context.Context, actor authz.Actor, serverID uuid.UUID, in AgentUpdateStartRequest) (AgentInstallJob, error) {
	if serverID == uuid.Nil {
		return AgentInstallJob{}, &ValidationError{Msg: "server is required"}
	}
	if err := validateAgentUpdateStartRequest(in); err != nil {
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
	row, token, err := load(ctx, actor, serverID)
	if err != nil {
		return AgentInstallJob{}, err
	}
	host := net.ParseIP(row.Host)
	if row.ServerID != serverID || host == nil || host.To4() == nil || host.To4().String() != row.Host || row.Port < 1 || row.Port > 65535 || token == "" {
		return AgentInstallJob{}, ErrAgentUpdateUnavailable
	}
	tlsExists := s.agentUpdateTLSExists
	if tlsExists == nil {
		tlsExists = s.registeredAgentTLSExists
	}
	exists, err := tlsExists(ctx, actor, serverID)
	if err != nil {
		return AgentInstallJob{}, ErrAgentUpdateUnavailable
	}
	if exists {
		return AgentInstallJob{}, ErrAgentUpdateTLSAlreadyConfigured
	}
	binaryLoader := s.agentUpdateBinary
	if binaryLoader == nil {
		binaryLoader = s.loadAgentInstallBinary
	}
	binary, err := binaryLoader()
	if err != nil {
		return AgentInstallJob{}, errors.New("trusted agent artifact unavailable")
	}
	credentials := s.agentUpdateCredentials
	if credentials == nil {
		credentials = newAgentUpdateTLSCredentials
	}
	certificate, privateKey, err := credentials(row.Host)
	if err != nil || validateAgentTLSConfig(row.Host, AgentTLSConfig{SecurePort: agentInstallHTTPSPort, TrustMode: AgentTLSTrustCustom, CAPEM: certificate}) != nil {
		return AgentInstallJob{}, errors.New("could not prepare agent TLS trust")
	}
	request := AgentUpdateRequest{
		Host: row.Host, SSHPort: in.SSHPort, Username: "root", Password: in.Password,
		Binary: binary, AgentToken: token, SecurePort: agentInstallHTTPSPort,
		TLSCertificate: certificate, TLSPrivateKey: privateKey,
	}
	request.hostKeyTrust = func(ctx context.Context, fingerprint string) error {
		return s.trustAgentSSHHostKey(ctx, actor, serverID, row.Host, in.SSHPort, fingerprint)
	}
	go s.runServerAgentUpdate(actor, job, row, request)
	reservationOwned = false
	return job.read(), nil
}

func (s *Service) requireAgentUpdate(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error {
	if s.requireAgentUpdatePermissions != nil {
		return s.requireAgentUpdatePermissions(ctx, actor, serverID)
	}
	if s.Inv == nil {
		return errors.New("agent update authorization unavailable")
	}
	return s.Inv.RequireServerManageAndConfigSecrets(ctx, actor, serverID)
}

func (s *Service) loadRegisteredAgentForUpdate(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (db.ServerAgent, string, error) {
	row, err := s.agentRow(ctx, actor, serverID)
	if errors.Is(err, store.ErrNotFound) {
		return db.ServerAgent{}, "", ErrAgentUnprovisioned
	}
	if err != nil {
		return db.ServerAgent{}, "", ErrAgentUpdateUnavailable
	}
	token, err := s.Sealer.Open(row.TokenSealed, row.ServerID[:])
	if err != nil {
		return db.ServerAgent{}, "", ErrAgentUpdateUnavailable
	}
	return row, string(token), nil
}

func (s *Service) registeredAgentTLSExists(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (bool, error) {
	_, err := s.agentTLSRow(ctx, actor, serverID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) runServerAgentUpdate(actor authz.Actor, job *agentInstallJob, original db.ServerAgent, request AgentUpdateRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), agentUpdateJobTimeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			job.update("failed", "failed", "Agent update failed; remote output was not retained")
		}
		request.Password, request.AgentToken = "", ""
		request.TLSPrivateKey, request.TLSCertificate, request.Binary = nil, nil, nil
		s.releaseAgentInstallJob(job, false)
	}()
	job.update("running", "connecting", "Connecting to the SSH host with remembered first-use trust")
	run := s.agentUpdateRun
	if run == nil {
		run = func(ctx context.Context, request AgentUpdateRequest, progress func(string)) error {
			progress("transferring")
			installRequest := AgentInstallRequest{
				Host: request.Host, Port: request.SSHPort, User: request.Username, Password: request.Password,
				ExpectedHostKey: request.ExpectedHostKey, hostKeyTrust: request.hostKeyTrust,
			}
			return RunAgentUpdate(ctx, request, agentInstallDialer(installRequest), nil)
		}
	}
	if err := run(ctx, request, func(stage string) { job.update("running", stage, agentUpdateStageMessage(stage)) }); err != nil {
		job.update("failed", "failed", "Agent update failed; rollback may require target inspection")
		return
	}
	job.update("running", "registering", "Recording verified agent HTTPS trust")
	persist := s.agentUpdatePersistTLS
	if persist == nil {
		persist = s.persistUpdatedAgentTLS
	}
	if err := persist(ctx, actor, original, request.SecurePort, request.TLSCertificate); err != nil {
		job.update("failed", "failed", "Agent is healthy but TLS trust registration failed; inspect the target before retrying")
		return
	}
	job.update("succeeded", "complete", "Agent update and authenticated HTTPS health verified")
}

func newAgentUpdateTLSCredentials(host string) ([]byte, []byte, error) {
	return newAgentInstallTLSCredentials(host)
}

func (s *Service) persistUpdatedAgentTLS(ctx context.Context, actor authz.Actor, original db.ServerAgent, securePort uint16, caPEM []byte) error {
	if s.Store == nil || original.ServerID == uuid.Nil {
		return ErrAgentUpdateUnavailable
	}
	config := AgentTLSConfig{SecurePort: uint32(securePort), TrustMode: AgentTLSTrustCustom, CAPEM: caPEM}
	if err := validateAgentTLSConfig(original.Host, config); err != nil {
		return ErrAgentUpdateUnavailable
	}
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		return persistAgentUpdateTLSInTransaction(ctx, q, func(ctx context.Context, _ db.GetServerForUpdateRow) (agentUpdateTxChecker, error) {
			checker, err := access.Load(ctx, q, actor)
			return checker, err
		}, actor, original, config)
	})
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var different byte
	for i := range a {
		different |= a[i] ^ b[i]
	}
	return different == 0
}

func agentUpdateStageMessage(stage string) string {
	if strings.EqualFold(stage, "verifying") {
		return "Verifying authenticated HTTPS health"
	}
	return installStageMessage(stage)
}
