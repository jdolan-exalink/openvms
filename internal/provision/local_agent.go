package provision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const maxLocalAgentTokenBytes = 1024

var ErrAgentAlreadyProvisioned = errors.New("server agent is already registered")

// LocalAgentRegistration binds a local Compose agent and its explicit TLS trust
// anchor to an existing server. Token is accepted separately and never returned.
type LocalAgentRegistration struct {
	ServerID   uuid.UUID
	Host       string
	HTTPPort   uint32
	SecurePort uint32
	CAPEM      []byte
}

type localAgentRegistrationQueries interface {
	RegisterServerAgentIfAbsent(context.Context, db.RegisterServerAgentIfAbsentParams) (int64, error)
	UpsertServerAgentTLS(context.Context, db.UpsertServerAgentTLSParams) error
	InsertAudit(context.Context, db.InsertAuditParams) error
}

type localAgentTx func(context.Context, authz.Actor, uuid.UUID, []byte, time.Duration, func(db.GetServerRow, db.GetActorBySessionHashRow, localAgentRegistrationQueries) error) error

// RequireLocalAgentRegistration checks both server-scoped permissions before the CLI
// reads the private token file. The write transaction repeats authorization checks.
func (s *Service) RequireLocalAgentRegistration(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error {
	if actor.UserID == uuid.Nil || actor.TenantID == nil || *actor.TenantID == uuid.Nil || serverID == uuid.Nil {
		return errors.New("local agent registration is unavailable")
	}
	if s.requireLocalAgentPermissions != nil {
		return s.requireLocalAgentPermissions(ctx, actor, serverID)
	}
	if s.Inv == nil {
		return errors.New("local agent registration is unavailable")
	}
	return s.Inv.RequireServerManageAndConfigSecrets(ctx, actor, serverID)
}

// RegisterLocalAgent stores a new agent token and TLS trust atomically. Existing
// server-agent registrations are refused; replacement is a separate explicit flow.
func (s *Service) RegisterLocalAgent(ctx context.Context, actor authz.Actor, sessionHash []byte, sessionIdle time.Duration, in LocalAgentRegistration, token string) error {
	if len(sessionHash) != sha256.Size || sessionIdle < 0 || actor.SessionID == nil {
		return errors.New("active operator session is required")
	}
	if err := s.RequireLocalAgentRegistration(ctx, actor, in.ServerID); err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if err := validateLocalAgentRegistration(in, token); err != nil {
		return err
	}
	if s.Sealer == nil {
		return errors.New("local agent registration is unavailable")
	}
	sealed, err := s.Sealer.Seal([]byte(token), in.ServerID[:])
	if err != nil {
		return errors.New("could not protect local agent credential")
	}
	write := func(server db.GetServerRow, session db.GetActorBySessionHashRow, q localAgentRegistrationQueries) error {
		if session.ID != actor.UserID || session.TenantID == nil || actor.TenantID == nil || *session.TenantID != *actor.TenantID || session.SessionID != *actor.SessionID {
			return store.ErrNotFound
		}
		if server.ID != in.ServerID || server.TenantID == uuid.Nil || server.SiteID == uuid.Nil || actor.TenantID == nil || *actor.TenantID != server.TenantID {
			return store.ErrNotFound
		}
		rows, err := q.RegisterServerAgentIfAbsent(ctx, db.RegisterServerAgentIfAbsentParams{
			ServerID: in.ServerID, TenantID: server.TenantID, Host: in.Host,
			Port: int32(in.HTTPPort), Variant: "compose", TokenSealed: sealed, Version: agent.Version,
		})
		if err != nil {
			return err
		}
		if rows == 0 {
			return ErrAgentAlreadyProvisioned
		}
		ca := string(in.CAPEM)
		config := AgentTLSConfig{SecurePort: in.SecurePort, TrustMode: AgentTLSTrustCustom, CAPEM: in.CAPEM}
		if err := q.UpsertServerAgentTLS(ctx, db.UpsertServerAgentTLSParams{
			ServerID: in.ServerID, TenantID: server.TenantID, SecurePort: int32(in.SecurePort),
			TrustMode: AgentTLSTrustCustom, CaPem: &ca,
		}); err != nil {
			return err
		}
		return auditLocalAgentRegistration(ctx, q, actor, server.TenantID, in, config)
	}
	if s.localAgentTx != nil {
		return s.localAgentTx(ctx, actor, in.ServerID, sessionHash, sessionIdle, write)
	}
	if s.Store == nil {
		return errors.New("local agent registration is unavailable")
	}
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		server, err := q.GetServer(ctx, in.ServerID)
		if err != nil {
			return store.Classify(err)
		}
		session, err := q.GetActorBySessionHash(ctx, db.GetActorBySessionHashParams{TokenHash: sessionHash, IdleSeconds: sessionIdle.Seconds()})
		if err != nil {
			return store.Classify(err)
		}
		checker, err := access.Load(ctx, q, actor)
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
		return write(server, session, q)
	})
}

func validateLocalAgentRegistration(in LocalAgentRegistration, token string) error {
	if in.ServerID == uuid.Nil {
		return errors.New("server ID is required")
	}
	ip, err := netip.ParseAddr(in.Host)
	if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() || ip.String() != in.Host {
		return errors.New("agent host must be a canonical IPv4 literal")
	}
	if in.HTTPPort == 0 || in.HTTPPort > 65535 || in.SecurePort == 0 || in.SecurePort > 65535 || in.HTTPPort == in.SecurePort {
		return errors.New("agent HTTP and HTTPS ports must be distinct valid ports")
	}
	if len(token) < 32 || len(token) > maxLocalAgentTokenBytes || strings.ContainsAny(token, "\r\n\t ") {
		return errors.New("agent token file must contain one bounded token")
	}
	if err := validateAgentTLSConfig(in.Host, AgentTLSConfig{SecurePort: in.SecurePort, TrustMode: AgentTLSTrustCustom, CAPEM: in.CAPEM}); err != nil {
		return errors.New("agent TLS trust configuration is invalid")
	}
	return nil
}

func auditLocalAgentRegistration(ctx context.Context, q localAgentRegistrationQueries, actor authz.Actor, tenantID uuid.UUID, in LocalAgentRegistration, config AgentTLSConfig) error {
	fingerprint := sha256.Sum256(config.CAPEM)
	details, err := json.Marshal(map[string]any{
		"host": in.Host, "http_port": in.HTTPPort, "secure_port": config.SecurePort,
		"trust_mode": config.TrustMode, "ca_bundle_sha256": hex.EncodeToString(fingerprint[:]),
	})
	if err != nil {
		return fmt.Errorf("encode local agent audit metadata: %w", err)
	}
	return q.InsertAudit(ctx, db.InsertAuditParams{
		TenantID: &tenantID, ActorID: &actor.UserID, ActorName: actor.Username,
		Action: "SERVER_AGENT_LOCAL_REGISTERED", TargetType: "server_agent", TargetID: &in.ServerID,
		RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: details,
	})
}
