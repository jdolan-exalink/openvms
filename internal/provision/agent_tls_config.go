package provision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const maxAgentCAPEM = 64 << 10

const (
	AgentTLSTrustSystem = "system"
	AgentTLSTrustCustom = "custom"
)

var ErrAgentTLSNotConfigured = errors.New("agent TLS is not configured")

// AgentTLSConfig contains operator-managed trust and the independent secure port.
// CAPEM is public trust material and is never used as a private key or password store.
type AgentTLSConfig struct {
	SecurePort uint32 `json:"secure_port"`
	TrustMode  string `json:"trust_mode"`
	CAPEM      []byte `json:"ca_pem,omitempty"`
}

func validateAgentTLSConfig(host string, in AgentTLSConfig) error {
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || in.SecurePort < 1 || in.SecurePort > 65535 {
		return errors.New("invalid registered agent TLS configuration")
	}
	if len(in.CAPEM) > maxAgentCAPEM {
		return errors.New("agent CA bundle exceeds 64 KiB")
	}
	switch in.TrustMode {
	case AgentTLSTrustSystem:
		if len(in.CAPEM) != 0 {
			return errors.New("system trust mode cannot include a CA bundle")
		}
		_, err := buildAgentTLSConfig(ip.To4().String(), nil, true)
		return err
	case AgentTLSTrustCustom:
		if len(in.CAPEM) == 0 {
			return errors.New("custom trust mode requires a CA bundle")
		}
		_, err := buildAgentTLSConfig(ip.To4().String(), in.CAPEM, false)
		return err
	default:
		return errors.New("invalid agent TLS trust mode")
	}
}

func (s *Service) authorizeServerManage(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error {
	if s.requireServerManage != nil {
		return s.requireServerManage(ctx, actor, serverID)
	}
	_, err := s.Inv.RequireServerManage(ctx, actor, serverID)
	return err
}

// SetAgentTLSConfig changes only the registered server agent's HTTPS trust configuration.
func (s *Service) SetAgentTLSConfig(ctx context.Context, actor authz.Actor, serverID uuid.UUID, in AgentTLSConfig) (AgentTLSConfig, error) {
	if err := s.authorizeServerManage(ctx, actor, serverID); err != nil {
		return AgentTLSConfig{}, err
	}
	row, err := s.agentRow(ctx, actor, serverID)
	if errors.Is(err, store.ErrNotFound) {
		return AgentTLSConfig{}, ErrAgentUnprovisioned
	}
	if err != nil {
		return AgentTLSConfig{}, err
	}
	if err := validateAgentTLSConfig(row.Host, in); err != nil {
		return AgentTLSConfig{}, err
	}
	if err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		before, err := q.GetServerAgentTLS(ctx, serverID)
		var old *AgentTLSConfig
		if err == nil {
			v := configFromRow(before)
			old = &v
		} else if !errors.Is(store.Classify(err), store.ErrNotFound) {
			return err
		}
		if err := q.UpsertServerAgentTLS(ctx, db.UpsertServerAgentTLSParams{
			ServerID: serverID, TenantID: row.TenantID,
			SecurePort: int32(in.SecurePort), TrustMode: in.TrustMode, CaPem: nullCA(in),
		}); err != nil {
			return err
		}
		return auditAgentTLS(ctx, q, actor, row.TenantID, serverID, "SERVER_AGENT_TLS_SET", old, &in)
	}); err != nil {
		return AgentTLSConfig{}, err
	}
	return cloneAgentTLSConfig(in), nil
}

// GetAgentTLSConfig returns only the selected trust mode and public CA bundle.
func (s *Service) GetAgentTLSConfig(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (AgentTLSConfig, error) {
	if err := s.authorizeServerManage(ctx, actor, serverID); err != nil {
		return AgentTLSConfig{}, err
	}
	if _, err := s.agentRow(ctx, actor, serverID); errors.Is(err, store.ErrNotFound) {
		return AgentTLSConfig{}, ErrAgentUnprovisioned
	} else if err != nil {
		return AgentTLSConfig{}, err
	}
	row, err := s.agentTLSRow(ctx, actor, serverID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return AgentTLSConfig{}, ErrAgentTLSNotConfigured
		}
		return AgentTLSConfig{}, err
	}
	return configFromRow(row), nil
}

// DeleteAgentTLSConfig disables HTTPS credentials transport; it never falls back to HTTP here.
func (s *Service) DeleteAgentTLSConfig(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error {
	if err := s.authorizeServerManage(ctx, actor, serverID); err != nil {
		return err
	}
	agentRow, err := s.agentRow(ctx, actor, serverID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrAgentUnprovisioned
	}
	if err != nil {
		return err
	}
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		old, err := q.GetServerAgentTLS(ctx, serverID)
		if errors.Is(store.Classify(err), store.ErrNotFound) {
			return ErrAgentTLSNotConfigured
		}
		if err != nil {
			return err
		}
		previous := configFromRow(old)
		if _, err := q.DeleteServerAgentTLS(ctx, serverID); err != nil {
			return err
		}
		return auditAgentTLS(ctx, q, actor, agentRow.TenantID, serverID, "SERVER_AGENT_TLS_DELETED", &previous, nil)
	})
}

func (s *Service) agentTLSRow(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (db.ServerAgentTl, error) {
	var row db.ServerAgentTl
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		var err error
		row, err = q.GetServerAgentTLS(ctx, serverID)
		return store.Classify(err)
	})
	return row, err
}

func configFromRow(row db.ServerAgentTl) AgentTLSConfig {
	return AgentTLSConfig{SecurePort: uint32(row.SecurePort), TrustMode: row.TrustMode, CAPEM: caBytes(row.CaPem)}
}

func nullCA(in AgentTLSConfig) *string {
	if len(in.CAPEM) == 0 {
		return nil
	}
	value := string(in.CAPEM)
	return &value
}

func caBytes(value *string) []byte {
	if value == nil {
		return nil
	}
	return []byte(*value)
}

func cloneAgentTLSConfig(in AgentTLSConfig) AgentTLSConfig {
	in.CAPEM = append([]byte(nil), in.CAPEM...)
	return in
}

func auditAgentTLS(ctx context.Context, q *db.Queries, actor authz.Actor, tenantID, serverID uuid.UUID, action string, before, after *AgentTLSConfig) error {
	metadata := func(in *AgentTLSConfig) any {
		if in == nil {
			return nil
		}
		fingerprint := ""
		if len(in.CAPEM) > 0 {
			sum := sha256.Sum256(in.CAPEM)
			fingerprint = hex.EncodeToString(sum[:])
		}
		return map[string]any{"secure_port": in.SecurePort, "trust_mode": in.TrustMode, "ca_bundle_sha256": fingerprint}
	}
	data, err := json.Marshal(map[string]any{"before": metadata(before), "after": metadata(after)})
	if err != nil {
		return err
	}
	return q.InsertAudit(ctx, db.InsertAuditParams{
		TenantID: &tenantID, ActorID: &actor.UserID, ActorName: actor.Username,
		Action: action, TargetType: "server_agent_tls", TargetID: &serverID,
		RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: data,
	})
}

func (c AgentTLSConfig) String() string {
	return fmt.Sprintf("AgentTLSConfig{SecurePort:%d TrustMode:%s CAPEM:%s}", c.SecurePort, c.TrustMode, strings.Repeat("[redacted]", boolInt(len(c.CAPEM) > 0)))
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
