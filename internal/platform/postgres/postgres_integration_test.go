//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/platform/postgres"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
	"github.com/jdolan-exalink/openvms/migrations"
)

func TestMigrationsApplyAndAreIdempotent(t *testing.T) {
	ctx := context.Background()
	url := pgtest.Start(t)
	log := pgtest.Discard()

	v1, err := postgres.Migrate(ctx, url, migrations.FS, log)
	if err != nil {
		t.Fatal(err)
	}
	if v1 < 1 {
		t.Fatalf("expected at least one migration, got version %d", v1)
	}
	v2, err := postgres.Migrate(ctx, url, migrations.FS, log)
	if err != nil {
		t.Fatalf("second run should be a no-op: %v", err)
	}
	if v1 != v2 {
		t.Fatalf("version changed on re-run: %d -> %d", v1, v2)
	}

	pool, err := postgres.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if v, err := postgres.SchemaVersion(ctx, pool); err != nil || v != v1 {
		t.Fatalf("SchemaVersion as app role = %d, %v; want %d", v, err, v1)
	}
	var role string
	if err := pool.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil || role != "openvms_app" {
		t.Fatalf("pool runs as %q (%v), want openvms_app", role, err)
	}

	for _, ext := range []string{"pg_trgm", "pgcrypto"} {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_extension WHERE extname = $1", ext).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("extension %s not installed", ext)
		}
	}
}
