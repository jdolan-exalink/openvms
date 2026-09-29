//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/notify"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// setupRulesTest returns the shared request helper plus a tenant user holding notifications.manage.
func setupRulesTest(t *testing.T) (*testAlarmsEnv, string) {
	t.Helper()
	env := demofix.Setup(t)
	pub := &testPublisher{}
	handlers := &api.Handlers{
		Inv:    env.Svc,
		Rules:  rules.NewService(env.Store, pub, pgtest.Discard()),
		Notify: notify.NewService(env.Store, env.Sealer, notify.Deps{}, pgtest.Discard()),
		Log:    pgtest.Discard(),
	}
	router, err := api.NewRouter(handlers, pgtest.Discard(), api.Options{Queries: db.New(env.Pool)})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	ctx := context.Background()
	manager, token, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "reglas")
	if err != nil {
		t.Fatal(err)
	}
	tenantID := env.Demo.TenantID
	if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
		SubjectType: "user", SubjectID: manager.UserID, Permission: authz.NotificationsManage,
		Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenantID,
	}); err != nil {
		t.Fatal(err)
	}
	return &testAlarmsEnv{Env: env, server: ts, pub: pub}, token
}

func TestRulesCRUDAndRBAC(t *testing.T) {
	te, token := setupRulesTest(t)
	cam := te.Cameras["frigate-h01/acceso_norte"]

	// Operators without notifications.manage cannot see rules.
	if code, body := te.request("GET", "/api/v1/rules", te.Demo.Tokens["operador"], nil); code != 403 {
		t.Fatalf("operador list rules code = %d, want 403: %s", code, body)
	}

	// Validation errors map to 400.
	if code, body := te.request("POST", "/api/v1/rules", token, map[string]any{
		"name": "  ", "trigger_type": "event",
	}); code != 400 {
		t.Fatalf("blank name code = %d, want 400: %s", code, body)
	}

	code, body := te.request("POST", "/api/v1/rules", token, map[string]any{
		"name":         "Personas en acceso norte",
		"trigger_type": "event",
		"conditions":   map[string]any{"camera_ids": []string{cam.ID.String()}, "site_ids": []string{cam.SiteID.String()}, "labels": []string{"person"}},
		"actions":      map[string]any{"create_alarm": true, "notify_in_app": true, "severity": "warning"},
	})
	if code != 201 {
		t.Fatalf("create rule code = %d, want 201: %s", code, body)
	}
	var created gen.Rule
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	if !created.Enabled {
		t.Fatalf("rule should default to enabled")
	}
	if created.Conditions.Labels == nil || (*created.Conditions.Labels)[0] != "person" {
		t.Fatalf("labels not round-tripped: %+v", created.Conditions)
	}
	if created.Conditions.SiteIds == nil || len(*created.Conditions.SiteIds) != 1 || (*created.Conditions.SiteIds)[0] != cam.SiteID {
		t.Fatalf("site_ids not round-tripped: %+v", created.Conditions)
	}

	path := "/api/v1/rules/" + created.Id.String()
	code, body = te.request("PATCH", path, token, map[string]any{"enabled": false, "name": "Renombrada"})
	if code != 200 {
		t.Fatalf("update rule code = %d, want 200: %s", code, body)
	}
	var updated gen.Rule
	_ = json.Unmarshal(body, &updated)
	if updated.Enabled || updated.Name != "Renombrada" {
		t.Fatalf("update not applied: %+v", updated)
	}

	code, body = te.request("GET", "/api/v1/rules", token, nil)
	if code != 200 {
		t.Fatalf("list rules code = %d: %s", code, body)
	}
	var list gen.RulesList
	_ = json.Unmarshal(body, &list)
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(list.Items))
	}

	if code, body := te.request("DELETE", path, token, nil); code != 204 {
		t.Fatalf("delete rule code = %d, want 204: %s", code, body)
	}
	if code, body := te.request("GET", path, token, nil); code != 404 {
		t.Fatalf("get deleted rule code = %d, want 404: %s", code, body)
	}
}

func TestNotificationsEmptyInbox(t *testing.T) {
	te, token := setupRulesTest(t)

	code, body := te.request("GET", "/api/v1/notifications?unread_only=true", token, nil)
	if code != 200 {
		t.Fatalf("list notifications code = %d, want 200: %s", code, body)
	}
	var list gen.NotificationList
	_ = json.Unmarshal(body, &list)
	if len(list.Items) != 0 || list.UnreadCount != 0 {
		t.Fatalf("expected empty inbox, got %+v", list)
	}

	code, body = te.request("POST", "/api/v1/notifications/read-all", token, nil)
	if code != 200 {
		t.Fatalf("read-all code = %d, want 200: %s", code, body)
	}
}
