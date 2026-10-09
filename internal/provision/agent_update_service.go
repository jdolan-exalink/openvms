package provision

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
var ErrAgentUpdateTLSAlreadyConfigured = errors.New("registered agent TLS configuration changed during update")

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
	var registeredTLS AgentTLSConfig
	preserveTLS := false
	if s.agentUpdateTLSConfig != nil || s.Store != nil {
		tlsConfigLoader := s.agentUpdateTLSConfig
		if tlsConfigLoader == nil {
			tlsConfigLoader = s.registeredAgentTLSConfig
		}
		registeredTLS, err = tlsConfigLoader(ctx, actor, serverID)
		if err == nil {
			preserveTLS = true
		} else if !errors.Is(err, ErrAgentTLSNotConfigured) && !errors.Is(err, store.ErrNotFound) {
			return AgentInstallJob{}, ErrAgentUpdateUnavailable
		}
	} else if s.agentUpdateTLSExists != nil {
		exists, checkErr := s.agentUpdateTLSExists(ctx, actor, serverID)
		if checkErr != nil {
			return AgentInstallJob{}, ErrAgentUpdateUnavailable
		}
		if exists {
			return AgentInstallJob{}, ErrAgentUpdateTLSAlreadyConfigured
		}
	} else {
		return AgentInstallJob{}, ErrAgentUpdateUnavailable
	}
	if preserveTLS && validateAgentTLSConfig(row.Host, registeredTLS) != nil {
		return AgentInstallJob{}, ErrAgentUpdateUnavailable
	}
	if preserveTLS {
		preflightHealth := s.agentUpdatePreflightHealth
		if preflightHealth == nil && s.Store != nil {
			preflightHealth = checkRegisteredAgentUpdateHealth
		}
		if preflightHealth == nil {
			return AgentInstallJob{}, ErrAgentUpdateUnavailable
		}
		if err := preflightHealth(ctx, row.Host, uint16(registeredTLS.SecurePort), token, registeredTLS); err != nil {
			return AgentInstallJob{}, ErrAgentUpdateUnavailable
		}
	}
	binaryLoader := s.agentUpdateBinary
	if binaryLoader == nil {
		binaryLoader = s.loadAgentInstallBinary
	}
	binary, err := binaryLoader()
	if err != nil {
		return AgentInstallJob{}, errors.New("trusted agent artifact unavailable")
	}
	var certificate, privateKey []byte
	securePort := uint16(agentInstallHTTPSPort)
	trustMode := AgentTLSTrustCustom
	if preserveTLS {
		certificate = append([]byte(nil), registeredTLS.CAPEM...)
		securePort = uint16(registeredTLS.SecurePort)
		trustMode = registeredTLS.TrustMode
	} else {
		credentials := s.agentUpdateCredentials
		if credentials == nil {
			credentials = newAgentUpdateTLSCredentials
		}
		certificate, privateKey, err = credentials(row.Host)
		if err != nil || validateAgentTLSConfig(row.Host, AgentTLSConfig{SecurePort: uint32(securePort), TrustMode: AgentTLSTrustCustom, CAPEM: certificate}) != nil {
			return AgentInstallJob{}, errors.New("could not prepare agent TLS trust")
		}
	}
	request := AgentUpdateRequest{
		Host: row.Host, SSHPort: in.SSHPort, Username: "root", Password: in.Password,
		Binary: binary, AgentToken: token, SecurePort: securePort, TLSMode: trustMode, PreserveTLS: preserveTLS,
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

func (s *Service) registeredAgentTLSConfig(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (AgentTLSConfig, error) {
	row, err := s.agentTLSRow(ctx, actor, serverID)
	if errors.Is(err, store.ErrNotFound) {
		return AgentTLSConfig{}, ErrAgentTLSNotConfigured
	}
	if err != nil {
		return AgentTLSConfig{}, ErrAgentUpdateUnavailable
	}
	return configFromRow(row), nil
}

func (s *Service) runServerAgentUpdate(actor authz.Actor, job *agentInstallJob, original db.ServerAgent, request AgentUpdateRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), agentUpdateJobTimeout)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			s.failServerAgentUpdate(job, original.ServerID, nil)
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
		s.failServerAgentUpdate(job, original.ServerID, err)
		return
	}
	if request.PreserveTLS {
		job.update("running", "registering", "Confirming existing agent HTTPS trust remains unchanged")
		verify := s.agentUpdateVerifyTLS
		if verify == nil {
			verify = s.verifyAgentUpdateTLSUnchanged
		}
		config := AgentTLSConfig{SecurePort: uint32(request.SecurePort), TrustMode: request.TLSMode, CAPEM: request.TLSCertificate}
		if err := verify(ctx, actor, original, config); err != nil {
			s.failServerAgentUpdate(job, original.ServerID, newAgentUpdateFailure(agentUpdateStageRegistration, agentUpdateCodeTLSRegistrationFailed, agentUpdateRollbackNotAttempted))
			return
		}
	} else {
		job.update("running", "registering", "Recording verified agent HTTPS trust")
		persist := s.agentUpdatePersistTLS
		if persist == nil {
			persist = s.persistUpdatedAgentTLS
		}
		if err := persist(ctx, actor, original, request.SecurePort, request.TLSCertificate); err != nil {
			s.failServerAgentUpdate(job, original.ServerID, newAgentUpdateFailure(agentUpdateStageRegistration, agentUpdateCodeTLSRegistrationFailed, agentUpdateRollbackNotAttempted))
			return
		}
	}
	job.update("succeeded", "complete", "Agent update and authenticated HTTPS health verified")
}

func (s *Service) failServerAgentUpdate(job *agentInstallJob, serverID uuid.UUID, err error) {
	failure := safeAgentUpdateFailure(err)
	logger := s.Log
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("existing agent update failed",
		"server_id", serverID,
		"job_id", job.read().ID,
		"stage", failure.stage,
		"code", failure.code,
		"rollback", failure.rollback,
	)
	job.update("failed", "failed", safeAgentUpdateFailureMessage(failure))
}

func safeAgentUpdateFailure(err error) *agentUpdateFailure {
	var failure *agentUpdateFailure
	if !errors.As(err, &failure) || failure == nil || !validAgentUpdateFailureEnum(failure.stage, failure.code, failure.rollback) {
		return newAgentUpdateFailure(agentUpdateStageUnknown, agentUpdateCodeUnknown, agentUpdateRollbackUnknown)
	}
	return newAgentUpdateFailure(failure.stage, failure.code, failure.rollback)
}

func validAgentUpdateFailureEnum(stage, code, rollback string) bool {
	switch stage {
	case agentUpdateStageValidating:
		return code == agentUpdateCodeInvalidRequest && rollback == agentUpdateRollbackNotAttempted
	case agentUpdateStageConnecting:
		return (code == agentUpdateCodeUnavailable || code == agentUpdateCodeSSHHostKeyMismatch || code == agentUpdateCodeSSHConnectionFailed) && rollback == agentUpdateRollbackNotAttempted
	case agentUpdateStagePreflight:
		return (code == agentUpdateCodePreflightFailed || code == agentUpdateCodeServiceUnavailable) && rollback == agentUpdateRollbackNotAttempted
	case agentUpdateStageStaging:
		return code == agentUpdateCodeStagingUnavailable && rollback == agentUpdateRollbackNotAttempted || code == agentUpdateCodeStagingUncertain && rollback == agentUpdateRollbackUncertain
	case agentUpdateStageTransferring:
		return (code == agentUpdateCodeCancelled || code == agentUpdateCodeTransferFailed) && rollback == agentUpdateRollbackNotAttempted
	case agentUpdateStageActivating:
		return code == agentUpdateCodeActivationFailed && (rollback == agentUpdateRollbackRestored || rollback == agentUpdateRollbackUncertain)
	case agentUpdateStageVerifyingHealth:
		return code == agentUpdateCodeHealthCheckFailed && (rollback == agentUpdateRollbackRestored || rollback == agentUpdateRollbackUncertain)
	case agentUpdateStageRegistration:
		return code == agentUpdateCodeTLSRegistrationFailed && rollback == agentUpdateRollbackNotAttempted
	case agentUpdateStageCleanup:
		return code == agentUpdateCodeCleanupFailed && rollback == agentUpdateRollbackNotAttempted
	case agentUpdateStageUnknown:
		return code == agentUpdateCodeUnknown && rollback == agentUpdateRollbackUnknown
	default:
		return false
	}
}

func safeAgentUpdateFailureMessage(failure *agentUpdateFailure) string {
	if failure == nil {
		return "Agent update failed; inspect the target before retrying."
	}
	switch failure.code {
	case agentUpdateCodeSSHHostKeyMismatch:
		return "SSH host key changed since first use; verify the server identity before retrying."
	case agentUpdateCodeSSHConnectionFailed, agentUpdateCodeUnavailable:
		return "Could not establish SSH. Verify reachability and root SSH access, then inspect the target before retrying."
	case agentUpdateCodePreflightFailed:
		return "The target failed agent-update preflight; inspect OS, architecture, and service state before retrying."
	case agentUpdateCodeStagingUncertain:
		return "Staging ownership could not be confirmed; inspect the target before retrying."
	case agentUpdateCodeTransferFailed, agentUpdateCodeCancelled:
		return "Agent files could not be transferred; inspect the target before retrying."
	case agentUpdateCodeActivationFailed:
		if failure.rollback == agentUpdateRollbackRestored {
			return "Agent activation failed; the previous files were restored."
		}
		return "Agent activation failed and rollback could not be confirmed; inspect the target before retrying."
	case agentUpdateCodeHealthCheckFailed:
		if failure.rollback == agentUpdateRollbackRestored {
			return "HTTPS health verification failed; the previous agent files were restored."
		}
		return "HTTPS health verification failed and rollback could not be confirmed; inspect the target before retrying."
	case agentUpdateCodeCleanupFailed:
		return "Agent health was verified, but update staging cleanup failed; inspect the target before retrying."
	case agentUpdateCodeTLSRegistrationFailed:
		return "Agent HTTPS health succeeded, but trust registration failed; inspect server configuration before retrying."
	default:
		return "Agent update failed; inspect the target before retrying."
	}
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

type agentUpdateTLSVerifyQueries interface {
	GetServerForUpdate(context.Context, uuid.UUID) (db.GetServerForUpdateRow, error)
	GetServerAgentForUpdate(context.Context, uuid.UUID) (db.ServerAgent, error)
	GetServerAgentTLS(context.Context, uuid.UUID) (db.ServerAgentTl, error)
	InsertAudit(context.Context, db.InsertAuditParams) error
}

func verifyAgentUpdateTLSInTransaction(ctx context.Context, q agentUpdateTLSVerifyQueries, loadChecker agentUpdateTxCheckerLoader, actor authz.Actor, original db.ServerAgent, expected AgentTLSConfig) error {
	server, err := q.GetServerForUpdate(ctx, original.ServerID)
	if err != nil || server.ID != original.ServerID || server.TenantID != original.TenantID {
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
	currentAgent, err := q.GetServerAgentForUpdate(ctx, original.ServerID)
	if err != nil || !sameAgentUpdateBinding(currentAgent, original) {
		return ErrAgentUpdateUnavailable
	}
	currentTLS, err := q.GetServerAgentTLS(ctx, original.ServerID)
	if err != nil || currentTLS.TenantID != original.TenantID {
		return ErrAgentUpdateUnavailable
	}
	current := configFromRow(currentTLS)
	if !sameAgentTLSConfig(current, expected) {
		return ErrAgentUpdateUnavailable
	}
	return auditAgentTLS(ctx, q, actor, original.TenantID, original.ServerID, "SERVER_AGENT_BINARY_UPDATED_TLS_PRESERVED", &current, &current)
}

func (s *Service) verifyAgentUpdateTLSUnchanged(ctx context.Context, actor authz.Actor, original db.ServerAgent, expected AgentTLSConfig) error {
	if s.Store == nil || original.ServerID == uuid.Nil || validateAgentTLSConfig(original.Host, expected) != nil {
		return ErrAgentUpdateUnavailable
	}
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		return verifyAgentUpdateTLSInTransaction(ctx, q, func(ctx context.Context, _ db.GetServerForUpdateRow) (agentUpdateTxChecker, error) {
			return access.Load(ctx, q, actor)
		}, actor, original, expected)
	})
}

func sameAgentTLSConfig(a, b AgentTLSConfig) bool {
	return a.SecurePort == b.SecurePort && a.TrustMode == b.TrustMode && equalBytes(a.CAPEM, b.CAPEM)
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
