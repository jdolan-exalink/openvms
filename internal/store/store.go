package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ErrNotFound is returned when a row does not exist or is hidden by tenant isolation.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned on unique-constraint violations.
var ErrConflict = errors.New("conflict")

type Store struct {
	Pool *pgxpool.Pool
}

// TenantScope selects which tenants' rows a transaction may see through row-level security.
type TenantScope struct {
	All      bool
	TenantID uuid.UUID
}

// AllTenants is for platform users and background jobs.
var AllTenants = TenantScope{All: true}

// ScopeFor returns the row visibility for an actor: platform users see every tenant.
func ScopeFor(a authz.Actor) TenantScope {
	if a.IsPlatform() {
		return AllTenants
	}
	return TenantScope{TenantID: *a.TenantID}
}

// Tx runs fn in a transaction with the tenant settings row-level security reads.
func (s *Store) Tx(ctx context.Context, scope TenantScope, fn func(q *db.Queries) error) error {
	return s.TxRaw(ctx, scope, func(tx pgx.Tx) error { return fn(db.New(tx)) })
}

// TxRaw is Tx for code that builds SQL itself (dynamic search filters, bulk upserts).
// Use db.New(tx) inside it for generated queries.
func (s *Store) TxRaw(ctx context.Context, scope TenantScope, fn func(tx pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var err error
		if scope.All {
			_, err = tx.Exec(ctx, "SELECT set_config('app.all_tenants', 'on', true)")
		} else {
			_, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", scope.TenantID.String())
		}
		if err != nil {
			return fmt.Errorf("set tenant scope: %w", err)
		}
		return fn(tx)
	})
}

// Classify maps driver errors to ErrNotFound / ErrConflict, keeping others as they are.
func Classify(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: %s", ErrConflict, pgErr.ConstraintName)
	}
	return err
}
