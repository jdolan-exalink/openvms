//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
	"github.com/jdolan-exalink/openvms/migrations"
)

func TestCameraZoneSyncMigrationBackfillsExistingZonesIntoRetryableState(t *testing.T) {
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
	if _, err := provider.UpTo(ctx, 53); err != nil {
		t.Fatal(err)
	}
	var zoneID string
	if err := db.QueryRowContext(ctx, `
WITH t AS (INSERT INTO tenants (slug, name) VALUES ('t', 't') RETURNING id),
s AS (INSERT INTO sites (tenant_id, name) SELECT id, 's' FROM t RETURNING id, tenant_id),
f AS (INSERT INTO frigate_servers (tenant_id, site_id, name, base_url, username, password_sealed)
      SELECT tenant_id, id, 'f', 'http://f', 'u', '\x00'::bytea FROM s RETURNING id, tenant_id, site_id),
c AS (INSERT INTO cameras (tenant_id, site_id, server_id, remote_name, display_name)
      SELECT tenant_id, site_id, id, 'cam', 'cam' FROM f RETURNING id, tenant_id)
INSERT INTO camera_zones (tenant_id, camera_id, name, points, settings)
SELECT tenant_id, id, 'legacy', '[{"x":0.1,"y":0.1},{"x":0.9,"y":0.1},{"x":0.5,"y":0.9}]'::jsonb, '{"objects":["person"]}'::jsonb FROM c
RETURNING id`).Scan(&zoneID); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 54); err != nil {
		t.Fatal(err)
	}

	var zoneStatus, jobStatus, operation string
	var revision, jobRevision int64
	var desired string
	if err := db.QueryRowContext(ctx, `
SELECT z.sync_status, z.sync_revision, j.status, j.operation, j.desired_revision, j.desired_config::text
FROM camera_zones z JOIN camera_zone_sync_jobs j ON j.zone_id = z.id WHERE z.id = $1`, zoneID).
		Scan(&zoneStatus, &revision, &jobStatus, &operation, &jobRevision, &desired); err != nil {
		t.Fatalf("backfilled zone has no sync job: %v", err)
	}
	// No actor exists to authorize a remote write, so the backfill must be visibly retryable
	// instead of pending forever or silently writing to Frigate on deploy.
	if zoneStatus != "failed" || jobStatus != "failed" || operation != "upsert" || jobRevision != revision {
		t.Fatalf("backfill state zone=%q job=%q op=%q revision=%d/%d", zoneStatus, jobStatus, operation, revision, jobRevision)
	}
	if want := `"_ovms_name": "legacy"`; !strings.Contains(desired, want) || !strings.Contains(desired, `"_ovms_points"`) || !strings.Contains(desired, `"objects"`) {
		t.Fatalf("desired snapshot = %s", desired)
	}

	// Frigate 0.16 support was removed, so 'unsupported' is no longer a valid sync state.
	if _, err := db.ExecContext(ctx, `UPDATE camera_zones SET sync_status = 'unsupported' WHERE id = $1`, zoneID); err == nil {
		t.Fatal("camera_zones accepted the removed unsupported sync state")
	}
	if _, err := db.ExecContext(ctx, `UPDATE camera_zone_sync_jobs SET status = 'unsupported' WHERE zone_id = $1`, zoneID); err == nil {
		t.Fatal("camera_zone_sync_jobs accepted the removed unsupported status")
	}
}
