//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func TestOriginalFrigateConfig(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]
	serverID := cam.ServerID

	t.Run("without revisions the original is the current config read from Frigate", func(t *testing.T) {
		want, err := env.Svc.GetServerFrigateRaw(ctx, env.Admin, serverID)
		if err != nil || want == "" {
			t.Fatalf("raw config = %q, %v", want, err)
		}
		got, version, err := env.Svc.OriginalFrigateConfig(ctx, env.Admin, serverID)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("original = %q, want the current raw config", got)
		}
		if version == "" {
			t.Error("the Frigate version is missing")
		}
	})

	insert := func(kind, before, createdAt string) {
		t.Helper()
		err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO frigate_config_revisions (tenant_id, server_id, kind, before_yaml, after_yaml, created_at)
				VALUES ($1, $2, $3, $4, 'after: 1', $5::timestamptz)`, cam.TenantID, serverID, kind, before, createdAt)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// The earliest revision has no before_yaml (it was not readable then): it is skipped.
	insert("camera_patch", "", "2026-01-01T00:00:00Z")
	insert("raw_save", "original: true\n", "2026-01-02T00:00:00Z")
	insert("rollback", "later: true\n", "2026-01-03T00:00:00Z")

	t.Run("with revisions it is the earliest non-empty before_yaml, whatever the kind", func(t *testing.T) {
		got, _, err := env.Svc.OriginalFrigateConfig(ctx, env.Admin, serverID)
		if err != nil {
			t.Fatal(err)
		}
		if got != "original: true\n" {
			t.Fatalf("original = %q", got)
		}
	})

	t.Run("another server is not affected", func(t *testing.T) {
		other := env.Cameras["frigate-c01/muelle"].ServerID
		got, _, err := env.Svc.OriginalFrigateConfig(ctx, env.Admin, other)
		if err != nil || got == "original: true\n" || got == "" {
			t.Fatalf("other server original = %q, %v", got, err)
		}
	})

	t.Run("it needs servers.config.secrets", func(t *testing.T) {
		_, _, err := env.Svc.OriginalFrigateConfig(ctx, env.Actor(t, "operador"), serverID)
		if !errors.Is(err, access.ErrForbidden) && !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("err = %v, want forbidden", err)
		}
	})

	t.Run("storing a backup is audited with the digest and never the config", func(t *testing.T) {
		if err := env.Svc.AuditAgentBackupSaved(ctx, env.Admin, serverID, "abc123"); err != nil {
			t.Fatal(err)
		}
		var digest string
		err := env.Pool.QueryRow(ctx, `SELECT details->>'sha256' FROM audit_log WHERE action = 'AGENT_BACKUP_SAVED' AND target_id = $1`, serverID).Scan(&digest)
		if err != nil || digest != "abc123" {
			t.Fatalf("audit digest = %q, %v", digest, err)
		}
	})
}
