package faces

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
)

// SoleTenant is the organization a platform administrator acts on when a request names none:
// the only one that exists. With none or several it answers a validation error, so the caller
// is told to pass tenant_id.
func (s *Service) SoleTenant(ctx context.Context, actor authz.Actor) (uuid.UUID, error) {
	var ids []uuid.UUID
	err := s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM tenants WHERE deleted_at IS NULL ORDER BY created_at, id LIMIT 2`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil {
		return uuid.Nil, err
	}
	switch len(ids) {
	case 1:
		return ids[0], nil
	case 0:
		return uuid.Nil, invalid("tenant_id is required: no organization exists")
	}
	return uuid.Nil, invalid("tenant_id is required: several organizations exist")
}

// PersonTenant, WatchlistTenant and ServerTenant name the organization an object belongs to, so
// a request that carries only the object id can be authorized against it. The lookup runs under
// the actor's own row-level scope: another tenant's object is not found, never forbidden.
func (s *Service) PersonTenant(ctx context.Context, actor authz.Actor, personID uuid.UUID) (uuid.UUID, error) {
	return s.objectTenant(ctx, actor, `SELECT tenant_id FROM face_persons WHERE id = $1 AND deleted_at IS NULL`, personID)
}

func (s *Service) WatchlistTenant(ctx context.Context, actor authz.Actor, watchlistID uuid.UUID) (uuid.UUID, error) {
	return s.objectTenant(ctx, actor, `SELECT tenant_id FROM face_watchlists WHERE id = $1`, watchlistID)
}

func (s *Service) ServerTenant(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (uuid.UUID, error) {
	return s.objectTenant(ctx, actor, `SELECT tenant_id FROM frigate_servers WHERE id = $1 AND deleted_at IS NULL`, serverID)
}

func (s *Service) objectTenant(ctx context.Context, actor authz.Actor, query string, id uuid.UUID) (uuid.UUID, error) {
	var tenant uuid.UUID
	err := s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		return store.Classify(tx.QueryRow(ctx, query, id).Scan(&tenant))
	})
	return tenant, err
}

// readableTenants lists the organizations whose face data the actor may see (faces.view):
// the named one (not found for a member of another tenant), else the member's own, else every
// organization for a platform administrator. When candidates exist but none is readable the
// permission error is returned, so a caller without faces.view gets 403 rather than an empty page.
func readableTenants(ctx context.Context, tx pgx.Tx, c *access.Checker, actor authz.Actor, explicit *uuid.UUID) ([]uuid.UUID, error) {
	var candidates []uuid.UUID
	switch {
	case explicit != nil:
		if err := authorizeTenant(actor, *explicit); err != nil {
			return nil, err
		}
		candidates = []uuid.UUID{*explicit}
	case actor.TenantID != nil:
		candidates = []uuid.UUID{*actor.TenantID}
	default:
		rows, err := tx.Query(ctx, `SELECT id FROM tenants WHERE deleted_at IS NULL ORDER BY created_at, id`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			candidates = append(candidates, id)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	var out []uuid.UUID
	for _, id := range candidates {
		if c.Can(authz.FacesView, access.Tenant(id)) {
			out = append(out, id)
		}
	}
	if len(out) == 0 && len(candidates) > 0 {
		return nil, c.Require(authz.FacesView, access.Tenant(candidates[0]))
	}
	return out, nil
}
