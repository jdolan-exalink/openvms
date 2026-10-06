package provision

import (
	"context"
	"errors"
	"net/netip"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

var ErrAgentSSHHostKeyMismatch = errors.New("SSH host key does not match stored trust")

type agentSSHHostKeyTxQueries interface {
	GetServerForUpdate(context.Context, uuid.UUID) (db.GetServerForUpdateRow, error)
	ClaimServerAgentSSHHostKey(context.Context, db.ClaimServerAgentSSHHostKeyParams) (int64, error)
	GetServerAgentSSHHostKey(context.Context, db.GetServerAgentSSHHostKeyParams) (db.GetServerAgentSSHHostKeyRow, error)
}

type agentSSHHostKeyPermissionChecker interface {
	Require(authz.Permission, authz.Resource) error
}

type agentSSHHostKeyCheckerLoader func(context.Context, db.GetServerForUpdateRow) (agentSSHHostKeyPermissionChecker, error)

// persistAgentSSHHostKeyInTransaction claims the first observed SSH key, or
// compares it with the existing key, before the SSH client proceeds to password
// authentication. The unique key makes concurrent first contacts converge on one
// stored fingerprint; a losing connection must match that winner.
func persistAgentSSHHostKeyInTransaction(ctx context.Context, q agentSSHHostKeyTxQueries, loadChecker agentSSHHostKeyCheckerLoader, actor authz.Actor, serverID uuid.UUID, host string, port uint16, fingerprint string) error {
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is4() || ip.String() != host || serverID == uuid.Nil || port == 0 || !validFingerprint(fingerprint) {
		return &ValidationError{Msg: "invalid SSH host-key trust target"}
	}
	server, err := q.GetServerForUpdate(ctx, serverID)
	if err != nil {
		return store.Classify(err)
	}
	if server.ID != serverID || server.TenantID == uuid.Nil {
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
	_, err = q.ClaimServerAgentSSHHostKey(ctx, db.ClaimServerAgentSSHHostKeyParams{
		ServerID: serverID, TenantID: server.TenantID, Host: host, SshPort: int32(port), Fingerprint: fingerprint,
	})
	if err != nil {
		return err
	}
	stored, err := q.GetServerAgentSSHHostKey(ctx, db.GetServerAgentSSHHostKeyParams{ServerID: serverID, Host: host, SshPort: int32(port)})
	if err != nil {
		return store.Classify(err)
	}
	if stored.ServerID != serverID || stored.TenantID != server.TenantID || stored.Host != host || stored.SshPort != int32(port) || stored.Fingerprint != fingerprint {
		return ErrAgentSSHHostKeyMismatch
	}
	return nil
}

func (s *Service) persistAgentSSHHostKey(ctx context.Context, actor authz.Actor, serverID uuid.UUID, host string, port uint16, fingerprint string) error {
	if s.Store == nil || s.Inv == nil {
		return errors.New("SSH host-key trust storage unavailable")
	}
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		return persistAgentSSHHostKeyInTransaction(ctx, q, func(ctx context.Context, _ db.GetServerForUpdateRow) (agentSSHHostKeyPermissionChecker, error) {
			return access.Load(ctx, q, actor)
		}, actor, serverID, host, port, fingerprint)
	})
}

func (s *Service) trustAgentSSHHostKey(ctx context.Context, actor authz.Actor, serverID uuid.UUID, host string, port uint16, fingerprint string) error {
	if s.agentSSHHostKeyPersist != nil {
		return s.agentSSHHostKeyPersist(ctx, actor, serverID, host, port, fingerprint)
	}
	return s.persistAgentSSHHostKey(ctx, actor, serverID, host, port, fingerprint)
}
