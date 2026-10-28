//go:build integration

package inventory_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

// insertNVR adds a Provision recorder row straight into the table, next to the demo Frigate servers.
func insertNVR(t *testing.T, env *demofix.Env) uuid.UUID {
	t.Helper()
	cam := env.Cameras["frigate-h01/plaza"]
	id := uuid.New()
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(),
			`INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url, username, password_sealed, kind)
			 VALUES ($1, $2, $3, 'recorder', 'http://127.0.0.1:1', 'admin', '\x00', 'provision_nvr')`,
			id, cam.TenantID, cam.SiteID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func serverHealth(t *testing.T, env *demofix.Env, id uuid.UUID) (status string, checked *time.Time) {
	t.Helper()
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT status, last_checked_at FROM frigate_servers WHERE id = $1`, id).Scan(&status, &checked)
	})
	if err != nil {
		t.Fatal(err)
	}
	return status, checked
}

// The Frigate health poller must leave a Provision recorder alone, and keep polling Frigate servers.
func TestFrigateHealthPollerSkipsProvisionRecorders(t *testing.T) {
	env := demofix.Setup(t)
	nvr := insertNVR(t, env)
	poller := &inventory.HealthPoller{Svc: env.Svc, Interval: time.Second, Concurrency: 2}
	poller.PollOnce(context.Background())

	if status, checked := serverHealth(t, env, nvr); status != "unknown" || checked != nil {
		t.Fatalf("recorder health = %q checked=%v, want untouched (unknown, never checked)", status, checked)
	}
	cam := env.Cameras["frigate-h01/plaza"]
	if status, checked := serverHealth(t, env, cam.ServerID); status != "online" || checked == nil {
		t.Fatalf("Frigate server health = %q checked=%v, want online and checked", status, checked)
	}
}

func dropFirstItem(xml string) string {
	start := strings.Index(xml, "<item ")
	end := strings.Index(xml, "</item>")
	if start < 0 || end < 0 {
		return xml
	}
	return xml[:start] + xml[end+len("</item>"):]
}
