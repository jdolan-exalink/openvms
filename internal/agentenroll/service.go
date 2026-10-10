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

// errTokenNotFound marks the consume step of Enroll only; a not-found from any later step is
// an internal error, never a bad token.
var errTokenNotFound = errors.New("token not found")

// ErrInvalidToken is the only answer for a token that is unknown, already used or expired,
// so a caller cannot tell the cases apart and probe for live tokens.
var ErrInvalidToken = errors.New("agentenroll: invalid enrollment token")

// Authorizer decides whether an actor may mint a token for a server. It must report a
// server the actor cannot see as store.ErrNotFound.
type Authorizer interface {
	RequireServerManageAndConfigSecrets(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error
}

// Issuer signs CSRs (agentca.Service). Sign produces the certificate with an already loaded
// CA and does not record it: Enroll persists agentca.Issuance.Record itself, inside its
// transaction. LoadOrCreateCA may create and persist the CA through the issuer's own
// repository (its own pool connection), so Enroll calls it before opening the transaction and
// hands the result to Sign, which has no way to reach the repository.
type Issuer interface {
	LoadOrCreateCA(ctx context.Context) (*agentca.CA, error)
	Sign(ca *agentca.CA, csrPEM []byte, id agentca.Identity) (*agentca.Issuance, error)
}

// Service owns token creation and redemption.
type Service struct {
	Store *store.Store
	Authz Authorizer
	CA    Issuer
	TTL   time.Duration // defaults to DefaultTTL
}

// Token is a freshly created enrollment token. Value is shown once and never stored.
type Token struct {
	Value     string
	ExpiresAt time.Time
}

// hash is how tokens are stored: the hex SHA-256 of the token string.
func hash(token string) string { return hex.EncodeToString(identity.HashToken(token)) }

// CreateToken mints a token bound to serverID and replaces the server's unused one in a
// single upsert, so a server has exactly one live token even under concurrent calls (the
// newest wins). The expiry is computed by the database, whose clock also decides redemption.
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
	// The server is visible to the actor (checked above), so its tenant is the actor's.
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		srv, err := q.GetServer(ctx, serverID)
		if err != nil {
			return err
		}
		tok.ExpiresAt, err = q.UpsertAgentEnrollToken(ctx, db.UpsertAgentEnrollTokenParams{
			TenantID: srv.TenantID, ServerID: serverID, TokenHash: hash(tok.Value),
			TtlSeconds: ttl.Seconds(), CreatedBy: &actor.UserID,
		})
		if err != nil {
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
// any database work so a malformed request does not touch the token. Consuming the token,
// recording the certificate and writing the audit row then happen in one transaction: if any
// step fails everything rolls back, the token stays redeemable and no certificate was
// released (the signed certificate only leaves this function after the commit). Any token
// problem is ErrInvalidToken.
func (s *Service) Enroll(ctx context.Context, token string, csrPEM []byte) (*agentca.Issuance, error) {
	if err := agentca.ValidateCSR(csrPEM); err != nil {
		return nil, err
	}
	// Load (or create) the CA first: on a cold cache that takes its own pool connection, which
	// must not happen while the transaction below holds one and the token row lock.
	ca, err := s.CA.LoadOrCreateCA(ctx)
	if err != nil {
		return nil, err
	}
	var res *agentca.Issuance
	// The tenant is unknown until the token resolves, hence the all-tenants scope. That scope
	// also satisfies the tenant-scoped certificate and audit inserts below.
	err = s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		row, err := q.ConsumeAgentEnrollToken(ctx, hash(token))
		if err != nil {
			if errors.Is(store.Classify(err), store.ErrNotFound) {
				return errTokenNotFound
			}
			return err
		}
		res, err = s.CA.Sign(ca, csrPEM, agentca.Identity{TenantID: row.TenantID, ServerID: row.ServerID})
		if err != nil {
			return err
		}
		if err := agentca.RecordCertificate(ctx, q, res.Record); err != nil {
			return err
		}
		return audit(ctx, q, nil, row.TenantID, row.ServerID, "SERVER_AGENT_ENROLLED",
			map[string]any{"serial": res.Issued.Serial, "fingerprint": res.Issued.Fingerprint, "not_after": res.Issued.NotAfter})
	})
	if errors.Is(err, errTokenNotFound) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// SupersedeGrace is how long a certificate keeps working after a renewal replaced it. It
// covers an agent that did not receive the response and asks again with the old certificate,
// and open connections that still use it while the worker reconnects. Past it the old
// certificate is revoked, so each server has at most the newest certificate plus those still
// inside their grace.
const SupersedeGrace = 10 * time.Minute

// Renew issues a replacement certificate for an agent that authenticated with previousSerial.
// id must come from the verified client certificate, never from a request field.
//
// A certificate may be renewed only once half of its lifetime has passed (agents renew at two
// thirds), so a stolen or looping client cannot mint certificates at will; earlier requests
// fail with agentca.ErrRenewalTooEarly. Like Enroll, the check, recording the certificate,
// superseding the server's other certificates and the audit row happen in one transaction and
// the signed certificate is released only after the commit. Every check uses the database
// clock. The transaction is scoped to the agent's own tenant, so even a bug that passed the
// wrong server id could not reach another tenant's rows.
//
// The other live certificates of the server are not revoked at once but set to be revoked
// SupersedeGrace from now, so a retry with the old certificate still works for a while. An
// operator who suspects a key revokes the whole server (agentca.PgRepo.RevokeServerCertificates),
// which ends every certificate in the chain immediately, grace periods included.
func (s *Service) Renew(ctx context.Context, id agentca.Identity, previousSerial string, csrPEM []byte) (*agentca.Issuance, error) {
	if err := agentca.ValidateCSR(csrPEM); err != nil {
		return nil, err
	}
	ca, err := s.CA.LoadOrCreateCA(ctx)
	if err != nil {
		return nil, err
	}
	var res *agentca.Issuance
	err = s.Store.Tx(ctx, store.TenantScope{TenantID: id.TenantID}, func(q *db.Queries) error {
		renewable, err := q.LockRenewableAgentCertificate(ctx, db.LockRenewableAgentCertificateParams{
			Serial: previousSerial, ServerID: id.ServerID, TenantID: id.TenantID,
		})
		if errors.Is(store.Classify(err), store.ErrNotFound) {
			return agentca.ErrCertificateNotFound
		}
		if err != nil {
			return err
		}
		if !renewable {
			return agentca.ErrRenewalTooEarly
		}
		res, err = s.CA.Sign(ca, csrPEM, id)
		if err != nil {
			return err
		}
		if err := agentca.RecordCertificate(ctx, q, res.Record); err != nil {
			return err
		}
		superseded, err := q.SupersedeAgentCertificates(ctx, db.SupersedeAgentCertificatesParams{
			ServerID: id.ServerID, TenantID: id.TenantID, KeepSerial: res.Issued.Serial, GraceSeconds: SupersedeGrace.Seconds(),
		})
		if err != nil {
			return err
		}
		return audit(ctx, q, nil, id.TenantID, id.ServerID, "SERVER_AGENT_CERT_RENEWED", map[string]any{
			"serial": res.Issued.Serial, "previous_serial": previousSerial, "superseded": superseded,
			"fingerprint": res.Issued.Fingerprint, "not_after": res.Issued.NotAfter,
		})
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// audit records an enrollment event. audit_log.target_id is a uuid, so both events target the
// server under "server_agent" (as the other agent events do); the issued certificate is
// identified in details. actor is nil for the agent's own redemption.
func audit(ctx context.Context, q *db.Queries, actor *authz.Actor, tenantID, serverID uuid.UUID, action string, details map[string]any) error {
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	p := db.InsertAuditParams{
		TenantID: &tenantID, ActorName: "agent", Action: action,
		TargetType: "server_agent", TargetID: &serverID,
		RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: data,
	}
	if actor != nil {
		p.ActorID, p.ActorName = &actor.UserID, actor.Username
	}
	return q.InsertAudit(ctx, p)
}
