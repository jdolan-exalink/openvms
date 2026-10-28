//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
	"github.com/jdolan-exalink/openvms/migrations"
)

// Servers created before the kind existed are Frigate servers; only the two known kinds are accepted.
func TestServerKindMigrationKeepsExistingServersFrigate(t *testing.T) {
	url := pgtest.Start(t)
	ctx := context.Background()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 84); err != nil {
		t.Fatal(err)
	}
	var tenant, site string
	if err := db.QueryRowContext(ctx, `INSERT INTO tenants (name, slug) VALUES ('t', 't') RETURNING id`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO sites (tenant_id, name) VALUES ($1, 's') RETURNING id`, tenant).Scan(&site); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO frigate_servers (tenant_id, site_id, name, base_url, username, password_sealed) VALUES ($1, $2, 'old', 'http://x', 'u', '\x00')`, tenant, site); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 85); err != nil {
		t.Fatal(err)
	}
	var kind string
	if err := db.QueryRowContext(ctx, `SELECT kind FROM frigate_servers WHERE name = 'old'`).Scan(&kind); err != nil || kind != "frigate" {
		t.Fatalf("existing server kind = %q, %v; want frigate", kind, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO frigate_servers (tenant_id, site_id, name, base_url, username, password_sealed, kind) VALUES ($1, $2, 'nvr', 'http://y', 'u', '\x00', 'provision_nvr')`, tenant, site); err != nil {
		t.Fatalf("provision_nvr must be accepted: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO frigate_servers (tenant_id, site_id, name, base_url, username, password_sealed, kind) VALUES ($1, $2, 'bad', 'http://z', 'u', '\x00', 'other')`, tenant, site); err == nil {
		t.Fatal("an unknown kind must be rejected")
	}
	if _, err := provider.DownTo(ctx, 84); err != nil {
		t.Fatalf("down: %v", err)
	}
	if _, err := provider.UpTo(ctx, 85); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
