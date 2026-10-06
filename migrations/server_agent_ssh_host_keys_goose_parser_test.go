package migrations

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestServerAgentSSHHostKeyMigrationIsParsedAsCompleteStatements(t *testing.T) {
	goose.SetBaseFS(FS)
	t.Cleanup(func() { goose.SetBaseFS(nil) })
	migrations, err := goose.CollectMigrations(".", 33, 34)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 1 || migrations[0].Version != 34 {
		t.Fatalf("expected migration 34, got %#v", migrations)
	}
	sqlText, err := FS.ReadFile("00034_server_agent_ssh_host_keys.sql")
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ToLower(string(sqlText))
	for _, required := range []string{"create table public.server_agent_ssh_host_keys", "primary key (server_id, host, ssh_port)", "enable row level security", "force row level security", "create policy tenant_isolation", "using (app_tenant_visible(tenant_id))", "with check (app_tenant_visible(tenant_id))"} {
		if !strings.Contains(normalized, required) {
			t.Errorf("migration missing %q", required)
		}
	}
	if !strings.Contains(normalized, "-- +goose statementbegin") || !strings.Contains(normalized, "-- +goose statementend") {
		t.Fatal("migration must delimit its dollar-quoted validation block for Goose")
	}
	down := normalized[strings.Index(normalized, "-- +goose down"):]
	if !strings.Contains(down, "raise exception") {
		t.Fatal("down migration must always refuse: forced RLS can hide persisted host keys from the migration role")
	}
	if strings.Contains(down, "if exists") || strings.Contains(down, "select ") || strings.Contains(down, "drop table") || strings.Contains(down, "drop constraint") {
		t.Fatal("down migration must not conditionally inspect or delete trust data under RLS")
	}

	registerCaptureDriver.Do(func() { sql.Register("onvif_migration_capture", captureMigrationDriver{}) })
	db, err := sql.Open("onvif_migration_capture", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, direction := range []string{"up", "down"} {
		t.Run(direction, func(t *testing.T) {
			capturedMigrationMu.Lock()
			capturedMigrationSQL = nil
			capturedMigrationMu.Unlock()
			var err error
			if direction == "up" {
				err = migrations[0].Up(db)
			} else {
				err = migrations[0].Down(db)
			}
			if err != nil {
				t.Fatalf("Goose migration parser/%s: %v", direction, err)
			}
			capturedMigrationMu.Lock()
			statements := append([]string(nil), capturedMigrationSQL...)
			capturedMigrationMu.Unlock()
			if len(statements) < 2 || !strings.HasPrefix(strings.TrimSpace(statements[0]), "DO $$") || !strings.Contains(statements[0], "END $$;") {
				t.Fatalf("Goose %s parser split dollar-quoted SQL: %#v", direction, statements)
			}
			if direction == "down" && !strings.Contains(strings.ToLower(statements[0]), "raise exception") {
				t.Fatalf("Goose Down should execute one unconditional refusal block, got %#v", statements)
			}
		})
	}
}
