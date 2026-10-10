package agentca

import (
	"context"

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
		return q.InsertAgentCertificate(ctx, db.InsertAgentCertificateParams{
			Serial:      c.Serial,
			TenantID:    c.TenantID,
			ServerID:    c.ServerID,
			Fingerprint: c.Fingerprint,
			NotBefore:   c.NotBefore,
			NotAfter:    c.NotAfter,
		})
	})
}

// GetCertificate returns the record for a serial, or store.ErrNotFound.
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
		}
		return nil
	})
	return out, store.Classify(err)
}
