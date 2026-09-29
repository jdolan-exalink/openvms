//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

type testPublisher struct {
	mu        sync.Mutex
	published []string
}

func (p *testPublisher) Publish(_ context.Context, subject string, _ []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.published = append(p.published, subject)
	return nil
}

func (p *testPublisher) has(subject string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.published {
		if s == subject {
			return true
		}
	}
	return false
}

type testAlarmsEnv struct {
	*demofix.Env
	server *httptest.Server
	pub    *testPublisher
}

func setupAlarmsTest(t *testing.T) *testAlarmsEnv {
	t.Helper()
	env := demofix.Setup(t)
	pub := &testPublisher{}
	alarmSvc := &alarms.Service{
		Store: env.Store,
		Pub:   pub,
		Log:   pgtest.Discard(),
	}
	handlers := &api.Handlers{
		Inv:    env.Svc,
		Alarms: alarmSvc,
		Log:    pgtest.Discard(),
	}
	router, err := api.NewRouter(handlers, pgtest.Discard(), api.Options{
		Queries: db.New(env.Pool),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	return &testAlarmsEnv{
		Env:    env,
		server: ts,
		pub:    pub,
	}
}

func seedAlarm(t *testing.T, env *testAlarmsEnv, cam db.GetCameraRow, remoteID string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	eventID := uuid.New()
	alarmID := uuid.New()
	ctx := context.Background()

	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO events (id, tenant_id, site_id, server_id, camera_id, remote_id, severity, start_time, labels)
			VALUES ($1, $2, $3, $4, $5, $6, 'alert', now(), ARRAY['person']::text[])
		`, eventID, cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, remoteID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO alarms (id, tenant_id, site_id, camera_id, event_id, source, status)
			VALUES ($1, $2, $3, $4, $5, 'event', 'open')
		`, alarmID, cam.TenantID, cam.SiteID, cam.ID, eventID)
		return err
	})
	if err != nil {
		t.Fatalf("seedAlarm: %v", err)
	}
	return alarmID, eventID
}

func (te *testAlarmsEnv) request(method, path, token string, body any) (int, []byte) {
	var bodyReader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}
	req, _ := http.NewRequest(method, te.server.URL+path, bodyReader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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

func TestAlarmsListAndRBAC(t *testing.T) {
	te := setupAlarmsTest(t)
	// camAllowed is frigate-h01/acceso_norte which operador can access
	camAllowed := te.Cameras["frigate-h01/acceso_norte"]
	// camDenied is frigate-h01/plaza which operador cannot access
	camDenied := te.Cameras["frigate-h01/plaza"]

	alarm1, _ := seedAlarm(t, te, camAllowed, "review-1")
	alarm2, _ := seedAlarm(t, te, camDenied, "review-2")

	operadorToken := te.Demo.Tokens["operador"]

	// 1. Admin sees both alarms
	code, body := te.request("GET", "/api/v1/alarms", te.AdminToken, nil)
	if code != 200 {
		t.Fatalf("admin list alarms code = %d, want 200: %s", code, body)
	}
	var adminList gen.ListAlarms200JSONResponse
	_ = json.Unmarshal(body, &adminList)
	if len(adminList.Items) < 2 {
		t.Fatalf("admin expected at least 2 alarms, got %d", len(adminList.Items))
	}

	// 2. Operator only sees alarm on allowed camera
	code, body = te.request("GET", "/api/v1/alarms", operadorToken, nil)
	if code != 200 {
		t.Fatalf("operador list alarms code = %d, want 200: %s", code, body)
	}
	var opList gen.ListAlarms200JSONResponse
	_ = json.Unmarshal(body, &opList)
	if len(opList.Items) != 1 {
		t.Fatalf("operador expected 1 alarm, got %d", len(opList.Items))
	}
	if opList.Items[0].Id != alarm1 {
		t.Fatalf("operador expected alarm1 (%s), got %s", alarm1, opList.Items[0].Id)
	}

	// 3. Filter by camera_id
	code, body = te.request("GET", fmt.Sprintf("/api/v1/alarms?camera_id=%s", camAllowed.ID), te.AdminToken, nil)
	if code != 200 {
		t.Fatalf("filter by camera code = %d: %s", code, body)
	}
	var filteredList gen.ListAlarms200JSONResponse
	_ = json.Unmarshal(body, &filteredList)
	if len(filteredList.Items) != 1 || filteredList.Items[0].Id != alarm1 {
		t.Fatalf("expected filtered to return alarm1, got %+v", filteredList.Items)
	}

	// 4. Operador filtering on denied camera gets empty list (not 500)
	code, body = te.request("GET", fmt.Sprintf("/api/v1/alarms?camera_id=%s", camDenied.ID), operadorToken, nil)
	if code != 200 {
		t.Fatalf("operador filter denied camera code = %d: %s", code, body)
	}
	var deniedFilterList gen.ListAlarms200JSONResponse
	_ = json.Unmarshal(body, &deniedFilterList)
	if len(deniedFilterList.Items) != 0 {
		t.Fatalf("expected 0 alarms for denied camera, got %d", len(deniedFilterList.Items))
	}
	_ = alarm2
}

func TestAlarmsGetAndRBAC(t *testing.T) {
	te := setupAlarmsTest(t)
	camAllowed := te.Cameras["frigate-h01/acceso_norte"]
	camDenied := te.Cameras["frigate-h01/plaza"]

	alarmAllowed, _ := seedAlarm(t, te, camAllowed, "review-get-1")
	alarmDenied, _ := seedAlarm(t, te, camDenied, "review-get-2")

	operadorToken := te.Demo.Tokens["operador"]

	// Operator gets accessible alarm -> 200
	code, body := te.request("GET", fmt.Sprintf("/api/v1/alarms/%s", alarmAllowed), operadorToken, nil)
	if code != 200 {
		t.Fatalf("get allowed alarm code = %d, want 200: %s", code, body)
	}
	var a gen.Alarm
	_ = json.Unmarshal(body, &a)
	if a.Id != alarmAllowed || a.Status != "open" {
		t.Fatalf("unexpected alarm details: %+v", a)
	}

	// Operator gets denied camera alarm -> 403 Forbidden
	code, _ = te.request("GET", fmt.Sprintf("/api/v1/alarms/%s", alarmDenied), operadorToken, nil)
	if code != 403 {
		t.Fatalf("get denied alarm code = %d, want 403", code)
	}

	// Non-existent alarm -> 404 Not Found
	code, _ = te.request("GET", fmt.Sprintf("/api/v1/alarms/%s", uuid.New()), te.AdminToken, nil)
	if code != 404 {
		t.Fatalf("get nonexistent alarm code = %d, want 404", code)
	}
}

func TestAlarmsLifecycle(t *testing.T) {
	te := setupAlarmsTest(t)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	alarmID, _ := seedAlarm(t, te, cam, "review-life-1")
	operadorToken := te.Demo.Tokens["operador"]
	tenantID := te.Demo.TenantID.String()

	// 1. Acknowledge alarm
	code, body := te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/acknowledge", alarmID), operadorToken, nil)
	if code != 200 {
		t.Fatalf("acknowledge code = %d, want 200: %s", code, body)
	}
	var acked gen.Alarm
	_ = json.Unmarshal(body, &acked)
	if acked.Status != "acknowledged" || acked.AcknowledgedBy == nil {
		t.Fatalf("alarm not acknowledged properly: %+v", acked)
	}
	if !te.pub.has("alarm.acknowledged." + tenantID) {
		t.Fatalf("expected alarm.acknowledged.%s to be published", tenantID)
	}

	// Re-acknowledge is idempotent (200)
	code, _ = te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/acknowledge", alarmID), operadorToken, nil)
	if code != 200 {
		t.Fatalf("re-acknowledge code = %d, want 200", code)
	}

	// 2. List Assignees
	code, body = te.request("GET", fmt.Sprintf("/api/v1/alarms/%s/assignees", alarmID), operadorToken, nil)
	if code != 200 {
		t.Fatalf("list assignees code = %d: %s", code, body)
	}
	var assignees gen.ListAlarmAssignees200JSONResponse
	_ = json.Unmarshal(body, &assignees)
	if len(assignees.Items) == 0 {
		t.Fatalf("expected at least 1 assignee (operador), got 0")
	}
	var operadorID uuid.UUID
	for _, u := range assignees.Items {
		if u.Username == "operador" {
			operadorID = u.Id
		}
	}
	if operadorID == uuid.Nil {
		t.Fatalf("operador not found in assignees: %+v", assignees.Items)
	}

	// 3. Assign to valid user
	code, body = te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/assign", alarmID), operadorToken, gen.AlarmAssignInput{
		UserId: operadorID,
	})
	if code != 200 {
		t.Fatalf("assign code = %d: %s", code, body)
	}
	var assigned gen.Alarm
	_ = json.Unmarshal(body, &assigned)
	if assigned.AssignedTo == nil || *assigned.AssignedTo != operadorID {
		t.Fatalf("expected assigned_to to be %s, got %+v", operadorID, assigned.AssignedTo)
	}
	if !te.pub.has("alarm.assigned." + tenantID) {
		t.Fatalf("expected alarm.assigned.%s to be published", tenantID)
	}

	// Assign to invalid user (non-existent or lacking permissions) -> 400
	code, _ = te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/assign", alarmID), operadorToken, gen.AlarmAssignInput{
		UserId: uuid.New(),
	})
	if code != 400 {
		t.Fatalf("assign to invalid user code = %d, want 400", code)
	}

	// 4. Resolve alarm
	code, body = te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/resolve", alarmID), operadorToken, nil)
	if code != 200 {
		t.Fatalf("resolve code = %d: %s", code, body)
	}
	var resolved gen.Alarm
	_ = json.Unmarshal(body, &resolved)
	if resolved.Status != "resolved" || resolved.ResolvedBy == nil {
		t.Fatalf("expected resolved alarm, got %+v", resolved)
	}
	if !te.pub.has("alarm.resolved." + tenantID) {
		t.Fatalf("expected alarm.resolved.%s to be published", tenantID)
	}

	// Re-resolve is idempotent (200)
	code, _ = te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/resolve", alarmID), operadorToken, nil)
	if code != 200 {
		t.Fatalf("re-resolve code = %d, want 200", code)
	}

	// 5. Trying to acknowledge or assign a resolved alarm returns 409 Conflict
	code, _ = te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/acknowledge", alarmID), operadorToken, nil)
	if code != 409 {
		t.Fatalf("acknowledge resolved alarm code = %d, want 409", code)
	}
	code, _ = te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/assign", alarmID), operadorToken, gen.AlarmAssignInput{
		UserId: operadorID,
	})
	if code != 409 {
		t.Fatalf("assign resolved alarm code = %d, want 409", code)
	}
}

func TestAlarmsBulk(t *testing.T) {
	te := setupAlarmsTest(t)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	alarm1, _ := seedAlarm(t, te, cam, "review-bulk-1")
	alarm2, _ := seedAlarm(t, te, cam, "review-bulk-2")
	operadorToken := te.Demo.Tokens["operador"]

	// 1. Bulk acknowledge
	code, body := te.request("POST", "/api/v1/alarms/bulk", operadorToken, gen.AlarmBulkInput{
		Action:   "acknowledge",
		AlarmIds: []uuid.UUID{alarm1, alarm2},
	})
	if code != 200 {
		t.Fatalf("bulk acknowledge code = %d, want 200: %s", code, body)
	}
	var res gen.AlarmBulkResult
	_ = json.Unmarshal(body, &res)
	if res.Updated != 2 {
		t.Fatalf("expected 2 updated, got %d", res.Updated)
	}

	// Verify status of both alarms
	_, body1 := te.request("GET", fmt.Sprintf("/api/v1/alarms/%s", alarm1), operadorToken, nil)
	var a1 gen.Alarm
	_ = json.Unmarshal(body1, &a1)
	if a1.Status != "acknowledged" {
		t.Fatalf("alarm1 status = %s, want acknowledged", a1.Status)
	}

	// 2. Bulk resolve
	code, body = te.request("POST", "/api/v1/alarms/bulk", operadorToken, gen.AlarmBulkInput{
		Action:   "resolve",
		AlarmIds: []uuid.UUID{alarm1, alarm2},
	})
	if code != 200 {
		t.Fatalf("bulk resolve code = %d, want 200: %s", code, body)
	}
	_ = json.Unmarshal(body, &res)
	if res.Updated != 2 {
		t.Fatalf("expected 2 updated, got %d", res.Updated)
	}

	// 3. Bulk invalid action -> 400
	code, _ = te.request("POST", "/api/v1/alarms/bulk", operadorToken, gen.AlarmBulkInput{
		Action:   "unknown_action",
		AlarmIds: []uuid.UUID{alarm1},
	})
	if code != 400 {
		t.Fatalf("bulk invalid action code = %d, want 400", code)
	}
}
