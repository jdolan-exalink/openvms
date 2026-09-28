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
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// TestViewsAudit covers AG-1: saved views were never audited (internal/media/views.go had
// no audit call, unlike every other audited write). Creating, updating and deleting a view
// must each write one audit row with actor, tenant, target_type "view" and target_id.
func TestViewsAudit(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()

	operatorActor := env.Actor(t, "operador")
	if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
		SubjectType: "user", SubjectID: operatorActor.UserID, Permission: authz.ViewsCreatePrivate,
		Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &env.Demo.TenantID,
	}); err != nil {
		t.Fatal(err)
	}

	adapters := inventory.NewAdapters(env.Svc)
	mediaSvc := &media.Service{Store: env.Store, Adapters: adapters, Log: pgtest.Discard()}
	h := &api.Handlers{Inv: env.Svc, Media: mediaSvc, Log: pgtest.Discard()}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{Queries: db.New(env.Pool)})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	defer ts.Close()

	operator := env.Demo.Tokens["operador"]
	do := func(t *testing.T, method, path, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, method, ts.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+operator)
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

	countAudit := func(t *testing.T, action, targetID string) int {
		t.Helper()
		var n int
		if err := env.Pool.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE action = $1 AND target_type = 'view' AND target_id = $2`,
			action, targetID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	code, created := do(t, "POST", "/api/v1/views", `{"name":"Turno noche","layout":{"columns":1,"cells":[]}}`)
	if code != http.StatusCreated {
		t.Fatalf("create view: %d %v", code, created)
	}
	viewID, _ := created["id"].(string)
	if viewID == "" {
		t.Fatalf("create view: no id in %v", created)
	}
	if n := countAudit(t, "VIEW_CREATED", viewID); n != 1 {
		t.Errorf("VIEW_CREATED audit rows = %d, want 1", n)
	}

	code, body := do(t, "PUT", "/api/v1/views/"+viewID, `{"name":"Turno noche 2","layout":{"columns":1,"cells":[]}}`)
	if code != http.StatusOK {
		t.Fatalf("update view: %d %v", code, body)
	}
	if n := countAudit(t, "VIEW_UPDATED", viewID); n != 1 {
		t.Errorf("VIEW_UPDATED audit rows = %d, want 1", n)
	}

	code, body = do(t, "DELETE", "/api/v1/views/"+viewID, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete view: %d %v", code, body)
	}
	if n := countAudit(t, "VIEW_REMOVED", viewID); n != 1 {
		t.Errorf("VIEW_REMOVED audit rows = %d, want 1", n)
	}

	var actorName, tenantID string
	if err := env.Pool.QueryRow(ctx,
		`SELECT actor_name, tenant_id::text FROM audit_log WHERE action = 'VIEW_CREATED' AND target_id = $1`, viewID).
		Scan(&actorName, &tenantID); err != nil {
		t.Fatal(err)
	}
	if actorName != "operador" {
		t.Errorf("actor_name = %q, want operador", actorName)
	}
	if tenantID != env.Demo.TenantID.String() {
		t.Errorf("tenant_id = %q, want %q", tenantID, env.Demo.TenantID)
	}
}
