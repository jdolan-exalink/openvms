//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

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

// setupRulesTest returns the shared request helper plus a tenant user holding notifications.manage, rules.manage and rules.read.
func setupRulesTest(t *testing.T) (*testAlarmsEnv, string) {
	t.Helper()
	return setupRulesTestWith(t, notify.Deps{})
}

// setupRulesTestWith is setupRulesTest with explicit notification sender settings (WAHA).
func setupRulesTestWith(t *testing.T, deps notify.Deps) (*testAlarmsEnv, string) {
	t.Helper()
	env := demofix.Setup(t)
	pub := &testPublisher{}
	handlers := &api.Handlers{
		Inv:    env.Svc,
		Rules:  rules.NewService(env.Store, pub, pgtest.Discard()),
		Notify: notify.NewService(env.Store, env.Sealer, deps, pgtest.Discard()),
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
	for _, p := range []authz.Permission{authz.NotificationsManage, authz.RulesManage, authz.RulesRead} {
		if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: manager.UserID, Permission: p,
			Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenantID,
		}); err != nil {
			t.Fatal(err)
		}
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
	if created.Version != 1 {
		t.Fatalf("created version = %d, want 1", created.Version)
	}
	code, body = te.requestH("PATCH", path, token, map[string]any{"enabled": false, "name": "Renombrada"}, map[string]string{"If-Match": `"1"`})
	if code != 200 {
		t.Fatalf("update rule code = %d, want 200: %s", code, body)
	}
	var updated gen.Rule
	_ = json.Unmarshal(body, &updated)
	if updated.Version != 2 {
		t.Fatalf("updated version = %d, want 2", updated.Version)
	}
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

// requestH is request with extra headers.
func (te *testAlarmsEnv) requestH(method, path, token string, body any, headers map[string]string) (int, []byte) {
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, te.server.URL+path, rdr)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func createTestRule(t *testing.T, te *testAlarmsEnv, token string, conditions map[string]any) gen.Rule {
	t.Helper()
	code, body := te.request("POST", "/api/v1/rules", token, map[string]any{
		"name": "Regla", "trigger_type": "event", "conditions": conditions,
		"actions": map[string]any{"notify_in_app": true},
	})
	if code != 201 {
		t.Fatalf("create rule code = %d, want 201: %s", code, body)
	}
	var r gen.Rule
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRuleUpdateOptimisticLocking(t *testing.T) {
	te, token := setupRulesTest(t)
	r := createTestRule(t, te, token, map[string]any{"labels": []string{"person"}})
	path := "/api/v1/rules/" + r.Id.String()

	// Missing If-Match is allowed: the update proceeds without the version check and still bumps the version.
	if code, body := te.request("PATCH", path, token, map[string]any{"name": "Sin version"}); code != 200 {
		t.Fatalf("missing If-Match code = %d, want 200: %s", code, body)
	}
	// The unconditional update bumped the version to 2, so version 1 is now stale.
	if code, body := te.requestH("PATCH", path, token, map[string]any{"name": "x"}, map[string]string{"If-Match": "1"}); code != 409 {
		t.Fatalf("stale update after unconditional PATCH code = %d, want 409: %s", code, body)
	}
	// Current version wins and bumps the version.
	code, body := te.requestH("PATCH", path, token, map[string]any{"name": "Primera"}, map[string]string{"If-Match": "2"})
	if code != 200 {
		t.Fatalf("first update code = %d, want 200: %s", code, body)
	}
	// Replaying the stale version conflicts.
	if code, body := te.requestH("PATCH", path, token, map[string]any{"name": "Segunda"}, map[string]string{"If-Match": `"2"`}); code != 409 {
		t.Fatalf("stale update code = %d, want 409: %s", code, body)
	}
	// Unknown rule stays 404 even with a version.
	if code, body := te.requestH("PATCH", "/api/v1/rules/"+uuid.NewString(), token, map[string]any{"name": "x"}, map[string]string{"If-Match": "1"}); code != 404 {
		t.Fatalf("unknown rule code = %d, want 404: %s", code, body)
	}
	// Garbage version is a 400.
	if code, body := te.requestH("PATCH", path, token, map[string]any{"name": "x"}, map[string]string{"If-Match": "abc"}); code != 400 {
		t.Fatalf("garbage If-Match code = %d, want 400: %s", code, body)
	}
}

func TestRuleConditionValidationReturns400(t *testing.T) {
	te, token := setupRulesTest(t)
	cases := map[string]map[string]any{
		"server_ids on event": {"server_ids": []string{uuid.NewString()}},
		"bad schedule time":   {"schedule": map[string]any{"start_time": "25:99", "end_time": "06:00"}},
		"bad schedule day":    {"schedule": map[string]any{"days_of_week": []int{9}}},
		"bad timezone":        {"schedule": map[string]any{"timezone": "Mars/Olympus"}},
		"negative cooldown":   {"cooldown_seconds": -5},
		"huge cooldown":       {"cooldown_seconds": 999999},
		"unknown camera":      {"camera_ids": []string{uuid.NewString()}},
		"unknown group":       {"camera_group_ids": []string{uuid.NewString()}},
		"unknown map zone":    {"map_zone_ids": []string{uuid.NewString()}},
	}
	for name, cond := range cases {
		code, body := te.request("POST", "/api/v1/rules", token, map[string]any{
			"name": "Invalida", "trigger_type": "event", "conditions": cond,
		})
		if code != 400 {
			t.Errorf("%s: code = %d, want 400: %s", name, code, body)
		}
	}

	// Update validates the merged rule: switching a rule to server_offline while it keeps camera_ids.
	cam := te.Cameras["frigate-h01/acceso_norte"]
	r := createTestRule(t, te, token, map[string]any{"camera_ids": []string{cam.ID.String()}})
	code, body := te.requestH("PATCH", "/api/v1/rules/"+r.Id.String(), token,
		map[string]any{"trigger_type": "server_offline"}, map[string]string{"If-Match": "1"})
	if code != 400 {
		t.Fatalf("incompatible trigger change code = %d, want 400: %s", code, body)
	}
}

func TestRuleConditionsRoundTripPhase2Fields(t *testing.T) {
	te, token := setupRulesTest(t)
	r := createTestRule(t, te, token, map[string]any{
		"cooldown_seconds": 600,
		"schedule":         map[string]any{"days_of_week": []int{1, 2}, "start_time": "22:00", "end_time": "06:00", "timezone": "America/Argentina/Buenos_Aires"},
	})
	if r.Conditions.CooldownSeconds == nil || *r.Conditions.CooldownSeconds != 600 {
		t.Fatalf("cooldown_seconds not round-tripped: %+v", r.Conditions)
	}
	if r.Conditions.Schedule == nil || r.Conditions.Schedule.StartTime == nil || *r.Conditions.Schedule.StartTime != "22:00" {
		t.Fatalf("schedule not round-tripped: %+v", r.Conditions)
	}
}

func TestRulePermissionsSplitReadAndManage(t *testing.T) {
	te, token := setupRulesTest(t)
	ctx := context.Background()
	tenantID := te.Demo.TenantID
	r := createTestRule(t, te, token, map[string]any{"labels": []string{"person"}})

	mk := func(name string, perms ...authz.Permission) string {
		u, tok, err := bootstrap.TenantUser(ctx, te.Store, tenantID, name)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range perms {
			if _, err := te.Svc.CreateGrant(ctx, te.Admin, inventory.GrantInput{
				SubjectType: "user", SubjectID: u.UserID, Permission: p,
				Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenantID,
			}); err != nil {
				t.Fatal(err)
			}
		}
		return tok
	}
	reader := mk("lector-reglas", authz.RulesRead)
	legacy := mk("solo-notificaciones", authz.NotificationsManage)

	if code, body := te.request("GET", "/api/v1/rules", reader, nil); code != 200 {
		t.Fatalf("reader list code = %d, want 200: %s", code, body)
	}
	if code, body := te.request("GET", "/api/v1/rules/"+r.Id.String(), reader, nil); code != 200 {
		t.Fatalf("reader get code = %d, want 200: %s", code, body)
	}
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/v1/rules"},
		{"PATCH", "/api/v1/rules/" + r.Id.String()},
		{"DELETE", "/api/v1/rules/" + r.Id.String()},
	} {
		code, body := te.requestH(c.method, c.path, reader, map[string]any{"name": "n", "trigger_type": "event"}, map[string]string{"If-Match": "1"})
		if code != 403 {
			t.Errorf("reader %s %s code = %d, want 403: %s", c.method, c.path, code, body)
		}
	}
	// notifications.manage no longer opens the rules API.
	if code, body := te.request("GET", "/api/v1/rules", legacy, nil); code != 403 {
		t.Fatalf("notifications.manage list rules code = %d, want 403: %s", code, body)
	}
}
