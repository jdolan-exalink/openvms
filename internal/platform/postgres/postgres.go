// Package postgres opens connection pools and applies migrations.
package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// AppRole is the role application queries run as. It has no BYPASSRLS and no
// UPDATE/DELETE on audit_log, whatever the login role is (see migration 00002).
const AppRole = "openvms_app"

// Connect opens the application pool. Every connection switches to AppRole so that
// row-level security applies to the API and workers.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET ROLE "+AppRole)
		return err
	}
	return open(ctx, cfg)
}

func open(ctx context.Context, cfg *pgxpool.Config) (*pgxpool.Pool, error) {
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Migrate applies every pending migration using the login role (which owns the schema)
// on a short-lived pool, and returns the resulting version.
func Migrate(ctx context.Context, url string, migrations fs.FS, log *slog.Logger) (int64, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return 0, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 1
	pool, err := open(ctx, cfg)
	if err != nil {
		return 0, err
	}
	defer pool.Close()
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	if err != nil {
		return 0, fmt.Errorf("create migration provider: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return 0, fmt.Errorf("apply migrations: %w", err)
	}
	for _, r := range results {
		log.InfoContext(ctx, "migration applied", "version", r.Source.Version, "file", r.Source.Path, "duration_ms", r.Duration.Milliseconds())
	}
	return provider.GetDBVersion(ctx)
}

// Querier is satisfied by pools, connections and transactions.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// SchemaVersion returns the latest applied migration.
func SchemaVersion(ctx context.Context, q Querier) (int64, error) {
	var v int64
	err := q.QueryRow(ctx, "SELECT coalesce(max(version_id), 0) FROM goose_db_version WHERE is_applied").Scan(&v)
	return v, err
}
