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

func TestCameraAnalyticsSettingsMigrationUpAndDown(t *testing.T) {
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
	if _, err := provider.UpTo(ctx, 80); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name='camera_analytics_settings'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("table missing after up: %d %v", n, err)
	}
	var rls bool
	if err := db.QueryRowContext(ctx, `SELECT relforcerowsecurity FROM pg_class WHERE relname='camera_analytics_settings'`).Scan(&rls); err != nil || !rls {
		t.Fatalf("row level security must be forced: %v %v", rls, err)
	}
	if _, err := provider.DownTo(ctx, 79); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name='camera_analytics_settings'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("table still present after down: %d %v", n, err)
	}
	if _, err := provider.UpTo(ctx, 80); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
