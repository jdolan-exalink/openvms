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

func TestCameraConfigProfilesMigrationUpAndDown(t *testing.T) {
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
	if _, err := provider.UpTo(ctx, 77); err != nil {
		t.Fatal(err)
	}
	var tenant string
	if err := db.QueryRowContext(ctx, `INSERT INTO tenants (slug, name) VALUES ('t','t') RETURNING id`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO camera_config_profiles (tenant_id, name) VALUES ($1,'Exterior')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO camera_config_profiles (tenant_id, name) VALUES ($1,'EXTERIOR')`, tenant); err == nil {
		t.Fatal("profile names must be unique per tenant, case-insensitively")
	}
	if _, err := provider.DownTo(ctx, 76); err != nil {
		t.Fatalf("down: %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name='camera_config_profiles'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("table still present after down: %d %v", n, err)
	}
	if _, err := provider.UpTo(ctx, 77); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
