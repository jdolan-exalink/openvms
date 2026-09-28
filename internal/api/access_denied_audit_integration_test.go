//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// TestAccessDeniedAudit covers AG-2: a denied (403) request must itself be audited
// (ACCESS_DENIED, with actor, tenant, the permission, method, path and target where
// available), without auditing unauthenticated (401) noise. In the acceptance run, a user
// (juan) got 403 on PATCH /api/v1/servers/{id}, POST /api/v1/grants and POST /api/v1/views
// and nothing was logged; this exercises the same shape over HTTP.
func TestAccessDeniedAudit(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()

	adapters := inventory.NewAdapters(env.Svc)
	mediaSvc := &media.Service{Store: env.Store, Adapters: adapters, Log: pgtest.Discard()}
	h := &api.Handlers{Inv: env.Svc, Media: mediaSvc, Log: pgtest.Discard()}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{
		Queries: db.New(env.Pool),
		Media:   (&media.Gateway{Svc: mediaSvc, Actor: api.ActorFrom}).Routes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	defer ts.Close()

	servers, err := env.Svc.ListServers(ctx, env.Admin, nil)
	if err != nil || len(servers) == 0 {
		t.Fatalf("list servers: %v", err)
	}
	srv := servers[0]

	operator := env.Demo.Tokens["operador"]
	do := func(t *testing.T, method, path, token, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, method, ts.URL+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	countAudit := func(t *testing.T) int {
		t.Helper()
		var n int
		if err := env.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'ACCESS_DENIED'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("unauthenticated 401 is never audited", func(t *testing.T) {
		before := countAudit(t)
		code, _ := do(t, "GET", "/api/v1/cameras", "", "")
		if code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", code)
		}
		if after := countAudit(t); after != before {
			t.Errorf("ACCESS_DENIED rows = %d after a 401, want %d (unchanged)", after, before)
		}
	})

	t.Run("operator lacking servers.manage: 403, one audit row, and no side effect survives the denied tx", func(t *testing.T) {
		before := countAudit(t)
		code, body := do(t, "PATCH", "/api/v1/servers/"+srv.ID.String(), operator, `{"name":"hacked"}`)
		if code != http.StatusForbidden {
			t.Fatalf("PATCH server: %d %v", code, body)
		}
		if after := countAudit(t); after != before+1 {
			t.Fatalf("ACCESS_DENIED rows = %d, want %d", after, before+1)
		}
		// The denied write's own transaction rolled back (name unchanged); the audit row
		// above must still exist, proving it was written in a separate transaction.
		still, err := env.Svc.GetServer(ctx, env.Admin, srv.ID)
		if err != nil {
			t.Fatal(err)
		}
		if still.Name != srv.Name {
			t.Errorf("server name changed despite the denial: %q -> %q", srv.Name, still.Name)
		}

		var actorName, action, targetType, targetID, tenantID string
		var details []byte
		if err := env.Pool.QueryRow(ctx, `SELECT actor_name, action, target_type, target_id::text, tenant_id::text, details
			FROM audit_log WHERE action = 'ACCESS_DENIED' ORDER BY id DESC LIMIT 1`).
			Scan(&actorName, &action, &targetType, &targetID, &tenantID, &details); err != nil {
			t.Fatal(err)
		}
		var d map[string]any
		if err := json.Unmarshal(details, &d); err != nil {
			t.Fatal(err)
		}
		permission, _ := d["permission"].(string)
		gotMethod, _ := d["method"].(string)
		gotPath, _ := d["path"].(string)

		if actorName != "operador" {
			t.Errorf("actor_name = %q, want operador", actorName)
		}
		if tenantID != env.Demo.TenantID.String() {
			t.Errorf("tenant_id = %q, want %q", tenantID, env.Demo.TenantID)
		}
		if permission != "servers.manage" {
			t.Errorf("details.permission = %q, want servers.manage", permission)
		}
		if gotMethod != "PATCH" {
			t.Errorf("details.method = %q, want PATCH", gotMethod)
		}
		if gotPath != "/api/v1/servers/"+srv.ID.String() {
			t.Errorf("details.path = %q", gotPath)
		}
		if targetType != "server" || targetID != srv.ID.String() {
			t.Errorf("target = %s/%s, want server/%s", targetType, targetID, srv.ID)
		}
	})

	t.Run("media gateway denial is audited too", func(t *testing.T) {
		before := countAudit(t)
		code, _ := do(t, "GET", "/media/v1/cameras/"+env.Cameras["frigate-h01/plaza"].ID.String()+"/snapshot.jpg", operator, "")
		if code != http.StatusForbidden {
			t.Fatalf("GET snapshot: %d, want 403", code)
		}
		if after := countAudit(t); after != before+1 {
			t.Errorf("ACCESS_DENIED rows = %d, want %d", after, before+1)
		}
	})
}
