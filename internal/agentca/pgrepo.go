package agentca

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// PgRepo is the PostgreSQL implementation of Repository.
//
// agent_ca is global and has no row-level security, so it runs in an all-tenants
// transaction like other platform-level work. Certificate rows are tenant-scoped:
// writes use the certificate's own tenant, and lookups by serial run all-tenants
// because a listener knows the serial before it knows the tenant.
type PgRepo struct {
	Store *store.Store
}

var _ Repository = (*PgRepo)(nil)

func (r *PgRepo) GetCA(ctx context.Context) (*CARecord, error) {
	var rec *CARecord
	err := r.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		row, err := q.GetAgentCA(ctx)
		if err != nil {
			return err
		}
		rec = &CARecord{CertPEM: row.CertPem, KeySealed: row.KeySealed}
		return nil
	})
	if err != nil {
		if store.Classify(err) == store.ErrNotFound {
			return nil, ErrNoCA
		}
		return nil, err
	}
	return rec, nil
}

func (r *PgRepo) InsertCAIfAbsent(ctx context.Context, rec CARecord) (bool, error) {
	var n int64
	err := r.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) (err error) {
		n, err = q.InsertAgentCAIfAbsent(ctx, db.InsertAgentCAIfAbsentParams{CertPem: rec.CertPEM, KeySealed: rec.KeySealed})
		return err
	})
	return n == 1, err
}

func (r *PgRepo) InsertCertificate(ctx context.Context, c Certificate) error {
	return r.Store.Tx(ctx, store.TenantScope{TenantID: c.TenantID}, func(q *db.Queries) error {
		return RecordCertificate(ctx, q, c)
	})
}

// RecordCertificate inserts c using q, so the caller controls the transaction and the
// tenant scope (which must cover c.TenantID).
func RecordCertificate(ctx context.Context, q *db.Queries, c Certificate) error {
	var parent *string
	if c.ParentSerial != "" {
		parent = &c.ParentSerial
	}
	if err := q.InsertAgentCertificate(ctx, db.InsertAgentCertificateParams{
		Serial:       c.Serial,
		TenantID:     c.TenantID,
		ServerID:     c.ServerID,
		Fingerprint:  c.Fingerprint,
		NotBefore:    c.NotBefore,
		NotAfter:     c.NotAfter,
		ParentSerial: parent,
	}); err != nil {
		return fmt.Errorf("agentca: record certificate: %w", err)
	}
	return nil
}

// GetCertificate returns the record for a serial, or ErrCertificateNotFound.
func (r *PgRepo) GetCertificate(ctx context.Context, serial string) (Certificate, error) {
	var out Certificate
	err := r.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		row, err := q.GetAgentCertificate(ctx, serial)
		if err != nil {
			return err
		}
		out = Certificate{
			Serial: row.Serial, TenantID: row.TenantID, ServerID: row.ServerID,
			Fingerprint: row.Fingerprint, NotBefore: row.NotBefore, NotAfter: row.NotAfter, RevokedAt: row.RevokedAt,
			FirstUsedAt: row.FirstUsedAt,
		}
		if row.ParentSerial != nil {
			out.ParentSerial = *row.ParentSerial
		}
		return nil
	})
	if err != nil {
		err = store.Classify(err)
		if errors.Is(err, store.ErrNotFound) {
			return Certificate{}, ErrCertificateNotFound
		}
		return Certificate{}, err
	}
	return out, nil
}

// RevokeCertificate marks one certificate revoked and reports whether it did: false means
// the serial is unknown or was already revoked (the first revocation date is kept). The
// listener rejects the certificate from its next call on.
func (r *PgRepo) RevokeCertificate(ctx context.Context, serial string) (bool, error) {
	var n int64
	err := r.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) (err error) {
		n, err = q.RevokeAgentCertificate(ctx, serial)
		return err
	})
	return n > 0, err
}

// RevokeServerCertificates revokes every live certificate of a server and returns how many
// it revoked, for a decommissioned or compromised agent.
func (r *PgRepo) RevokeServerCertificates(ctx context.Context, serverID uuid.UUID) (int64, error) {
	var n int64
	err := r.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) (err error) {
		n, err = q.RevokeAgentCertificatesByServer(ctx, serverID)
		return err
	})
	return n, err
}

// ActivateCertificate records the first use of rec's certificate and revokes every other live
// certificate of its server, in one tenant-scoped transaction that first takes the server's
// certificate lock (the one renewals take), so it cannot interleave with a renewal. This is how
// a renewal supersedes the certificate it replaced: not when the new one is issued, but when the
// agent first authenticates with it. Clocks are the database's.
//
// It is idempotent: a certificate already used returns nil without changing anything. A
// certificate revoked in the meantime (a newer renewal replaced it while its first call was in
// flight) returns ErrCertificateRevoked; so does a serial with no row at all, which a listener
// can only meet if the row was deleted after it looked the certificate up, and which must be
// refused like a revocation.
func (r *PgRepo) ActivateCertificate(ctx context.Context, rec Certificate) error {
	return r.Store.Tx(ctx, store.TenantScope{TenantID: rec.TenantID}, func(q *db.Queries) error {
		if err := q.LockAgentServerCertificates(ctx, rec.ServerID.String()); err != nil {
			return err
		}
		cur, err := q.GetAgentCertificate(ctx, rec.Serial)
		if errors.Is(store.Classify(err), store.ErrNotFound) {
			return ErrCertificateRevoked
		}
		if err != nil {
			return err
		}
		if cur.RevokedAt != nil {
			return ErrCertificateRevoked
		}
		if cur.FirstUsedAt != nil {
			return nil
		}
		if _, err := q.MarkAgentCertificateUsed(ctx, rec.Serial); err != nil {
			return err
		}
		_, err = q.RevokeOtherAgentCertificates(ctx, db.RevokeOtherAgentCertificatesParams{
			ServerID: rec.ServerID, TenantID: rec.TenantID, KeepSerial: rec.Serial,
		})
		return err
	})
}
