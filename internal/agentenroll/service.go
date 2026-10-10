// Package agentenroll lets an operator mint a one-time enrollment token for a server and
// lets an edge agent redeem it, with a CSR, for a client certificate from the internal CA.
// The agent keeps its private key; only the CSR reaches the API.
package agentenroll

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// DefaultTTL is how long an enrollment token stays redeemable.
const DefaultTTL = 15 * time.Minute

// ErrInvalidToken is the only answer for a token that is unknown, already used or expired,
// so a caller cannot tell the cases apart and probe for live tokens.
var ErrInvalidToken = errors.New("agentenroll: invalid enrollment token")

// Authorizer decides whether an actor may mint a token for a server. It must report a
// server the actor cannot see as store.ErrNotFound.
type Authorizer interface {
	RequireServerManageAndConfigSecrets(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error
}

// Issuer signs a CSR for an identity (agentca.Service).
type Issuer interface {
	Issue(ctx context.Context, csrPEM []byte, id agentca.Identity) (*agentca.Issuance, error)
}

// Service owns token creation and redemption.
type Service struct {
	Store *store.Store
	Authz Authorizer
	CA    Issuer
	TTL   time.Duration    // defaults to DefaultTTL
	Now   func() time.Time // defaults to time.Now
}

// Token is a freshly created enrollment token. Value is shown once and never stored.
type Token struct {
	Value     string
	ExpiresAt time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// hash is how tokens are stored: the hex SHA-256 of the token string.
func hash(token string) string { return hex.EncodeToString(identity.HashToken(token)) }

// CreateToken mints a token bound to serverID and drops the server's older unused ones, so
// a server has at most one live token.
func (s *Service) CreateToken(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (Token, error) {
	if err := s.Authz.RequireServerManageAndConfigSecrets(ctx, actor, serverID); err != nil {
		return Token{}, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Token{}, err
	}
	tok := Token{Value: base64.RawURLEncoding.EncodeToString(raw)}
	ttl := s.TTL
	if ttl == 0 {
		ttl = DefaultTTL
	}
	tok.ExpiresAt = s.now().Add(ttl)
	// The server is visible to the actor (checked above), so its tenant is the actor's.
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		srv, err := q.GetServer(ctx, serverID)
		if err != nil {
			return err
		}
		if err := q.DeleteUnusedAgentEnrollTokensByServer(ctx, serverID); err != nil {
			return err
		}
		if err := q.InsertAgentEnrollToken(ctx, db.InsertAgentEnrollTokenParams{
			TenantID: srv.TenantID, ServerID: serverID, TokenHash: hash(tok.Value),
			ExpiresAt: tok.ExpiresAt, CreatedBy: &actor.UserID,
		}); err != nil {
			return err
		}
		return audit(ctx, q, &actor, srv.TenantID, serverID, "SERVER_AGENT_ENROLL_TOKEN_CREATED",
			map[string]any{"expires_at": tok.ExpiresAt})
	})
	if err != nil {
		return Token{}, store.Classify(err)
	}
	return tok, nil
}

// Enroll exchanges a token and a CSR for a client certificate. The CSR is validated before
// the token is touched so a malformed request does not burn it. The token is then consumed
// in one statement; if issuance fails afterwards the token stays consumed (fail closed) and
// the operator creates a new one. Any token problem is ErrInvalidToken.
func (s *Service) Enroll(ctx context.Context, token string, csrPEM []byte) (*agentca.Issuance, error) {
	if err := agentca.ValidateCSR(csrPEM); err != nil {
		return nil, err
	}
	var id agentca.Identity
	// The tenant is unknown until the token resolves, hence the all-tenants scope.
	err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		row, err := q.ConsumeAgentEnrollToken(ctx, hash(token))
		if err != nil {
			return err
		}
		id = agentca.Identity{TenantID: row.TenantID, ServerID: row.ServerID}
		return nil
	})
	if errors.Is(store.Classify(err), store.ErrNotFound) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	res, err := s.CA.Issue(ctx, csrPEM, id)
	if err != nil {
		return nil, err
	}
	err = s.Store.Tx(ctx, store.TenantScope{TenantID: id.TenantID}, func(q *db.Queries) error {
		return audit(ctx, q, nil, id.TenantID, id.ServerID, "SERVER_AGENT_ENROLLED",
			map[string]any{"serial": res.Issued.Serial, "not_after": res.Issued.NotAfter})
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// audit records an enrollment event; actor is nil for the agent's own redemption.
func audit(ctx context.Context, q *db.Queries, actor *authz.Actor, tenantID, serverID uuid.UUID, action string, details map[string]any) error {
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	p := db.InsertAuditParams{
		TenantID: &tenantID, ActorName: "agent", Action: action,
		TargetType: "server_agent_enroll_token", TargetID: &serverID,
		RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: data,
	}
	if actor != nil {
		p.ActorID, p.ActorName = &actor.UserID, actor.Username
	}
	return q.InsertAudit(ctx, p)
}
