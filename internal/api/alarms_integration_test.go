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

func TestAlarmLifecycleFull(t *testing.T) {
	te := setupAlarmsTest(t)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	alarmID, _ := seedAlarm(t, te, cam, "lifecycle-1")
	operadorToken := te.Demo.Tokens["operador"]
	adminToken := te.AdminToken

	// 1. Initial status is open
	code, body := te.request("GET", fmt.Sprintf("/api/v1/alarms/%s", alarmID), operadorToken, nil)
	if code != 200 {
		t.Fatalf("get alarm code = %d, want 200", code)
	}
	var a gen.Alarm
	_ = json.Unmarshal(body, &a)
	if a.Status != "open" {
		t.Fatalf("initial status = %s, want open", a.Status)
	}

	// 2. Acknowledge -> status = acknowledged
	code, body = te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/acknowledge", alarmID), operadorToken, nil)
	if code != 200 {
		t.Fatalf("acknowledge code = %d, want 200: %s", code, body)
	}
	_ = json.Unmarshal(body, &a)
	if a.Status != "acknowledged" || a.AcknowledgedBy == nil {
		t.Fatalf("acknowledged status = %s, want acknowledged", a.Status)
	}
	operadorID := *a.AcknowledgedBy

	// 3. Assign to user -> status = assigned
	code, body = te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/assign", alarmID), adminToken, gen.AlarmAssignInput{
		UserId: operadorID,
	})
	if code != 200 {
		t.Fatalf("assign code = %d, want 200: %s", code, body)
	}
	_ = json.Unmarshal(body, &a)
	if a.Status != "assigned" {
		t.Fatalf("assigned status = %s, want assigned", a.Status)
	}

	// 4. Investigate -> status = investigating
	comment := "checking camera feed"
	code, body = te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/investigate", alarmID), operadorToken, map[string]string{
		"comment": comment,
	})
	if code != 200 {
		t.Fatalf("investigate code = %d, want 200: %s", code, body)
	}
	_ = json.Unmarshal(body, &a)
	if a.Status != "investigating" {
		t.Fatalf("investigating status = %s, want investigating", a.Status)
	}

	// 5. Resolve -> status = resolved
	code, body = te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/resolve", alarmID), operadorToken, nil)
	if code != 200 {
		t.Fatalf("resolve code = %d, want 200: %s", code, body)
	}
	_ = json.Unmarshal(body, &a)
	if a.Status != "resolved" {
		t.Fatalf("resolved status = %s, want resolved", a.Status)
	}

	// 6. Close -> status = closed
	closeComment := "incident concluded"
	code, body = te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/close", alarmID), operadorToken, map[string]string{
		"comment": closeComment,
	})
	if code != 200 {
		t.Fatalf("close code = %d, want 200: %s", code, body)
	}
	_ = json.Unmarshal(body, &a)
	if a.Status != "closed" {
		t.Fatalf("closed status = %s, want closed", a.Status)
	}
	if a.ClosedBy == nil || *a.ClosedBy != operadorID {
		t.Fatalf("closed_by = %+v, want operador", a.ClosedBy)
	}
	if a.ClosedAt == nil {
		t.Fatalf("closed_at is nil")
	}

	// 7. Verify all transitions recorded
	code, body = te.request("GET", fmt.Sprintf("/api/v1/alarms/%s/transitions", alarmID), operadorToken, nil)
	if code != 200 {
		t.Fatalf("transitions code = %d, want 200: %s", code, body)
	}
	var transitions []gen.AlarmTransition
	_ = json.Unmarshal(body, &transitions)
	if len(transitions) != 5 {
		t.Fatalf("expected 5 transitions, got %d", len(transitions))
	}
	// Verify transition sequence
	expectedPairs := [][2]string{
		{"open", "acknowledged"},
		{"acknowledged", "assigned"},
		{"assigned", "investigating"},
		{"investigating", "resolved"},
		{"resolved", "closed"},
	}
	for i, pair := range expectedPairs {
		if transitions[i].FromStatus == nil || *transitions[i].FromStatus != pair[0] {
			t.Fatalf("transition %d from_status = %+v, want %s", i, transitions[i].FromStatus, pair[0])
		}
		if transitions[i].ToStatus == nil || *transitions[i].ToStatus != pair[1] {
			t.Fatalf("transition %d to_status = %+v, want %s", i, transitions[i].ToStatus, pair[1])
		}
	}
}

func TestAlarmComments(t *testing.T) {
	te := setupAlarmsTest(t)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	alarmID, _ := seedAlarm(t, te, cam, "comments-1")
	operadorToken := te.Demo.Tokens["operador"]

	// Add a comment
	code, body := te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/comments", alarmID), operadorToken, map[string]string{
		"comment": "Security guard dispatched to gate",
	})
	if code != 201 {
		t.Fatalf("add comment code = %d, want 201: %s", code, body)
	}
	var tr gen.AlarmTransition
	_ = json.Unmarshal(body, &tr)
	if tr.Comment != "Security guard dispatched to gate" {
		t.Fatalf("comment = %q", tr.Comment)
	}
	if tr.FromStatus != nil || tr.ToStatus != nil {
		t.Fatalf("comment transition must have nil statuses, got from=%+v to=%+v", tr.FromStatus, tr.ToStatus)
	}

	// Transitions list should contain this comment
	code, body = te.request("GET", fmt.Sprintf("/api/v1/alarms/%s/transitions", alarmID), operadorToken, nil)
	if code != 200 {
		t.Fatalf("transitions code = %d, want 200", code)
	}
	var list []gen.AlarmTransition
	_ = json.Unmarshal(body, &list)
	if len(list) != 1 || list[0].Comment != "Security guard dispatched to gate" {
		t.Fatalf("unexpected transitions list: %+v", list)
	}
}

func TestAlarmStatusGroupFilter(t *testing.T) {
	te := setupAlarmsTest(t)
	cam := te.Cameras["frigate-h01/acceso_norte"]
	alarmOpen, _ := seedAlarm(t, te, cam, "filter-open")
	alarmAck, _ := seedAlarm(t, te, cam, "filter-ack")
	alarmRes, _ := seedAlarm(t, te, cam, "filter-res")
	alarmClosed, _ := seedAlarm(t, te, cam, "filter-closed")

	operadorToken := te.Demo.Tokens["operador"]

	// Set statuses
	te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/acknowledge", alarmAck), operadorToken, nil)
	te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/resolve", alarmRes), operadorToken, nil)
	te.request("POST", fmt.Sprintf("/api/v1/alarms/%s/close", alarmClosed), operadorToken, nil)

	// Filter with status_group=active
	code, body := te.request("GET", "/api/v1/alarms?status_group=active", operadorToken, nil)
	if code != 200 {
		t.Fatalf("list alarms status_group=active code = %d, want 200", code)
	}
	var res struct {
		Items []gen.Alarm `json:"items"`
	}
	_ = json.Unmarshal(body, &res)

	foundIDs := make(map[uuid.UUID]bool)
	for _, it := range res.Items {
		foundIDs[it.Id] = true
	}

	if !foundIDs[alarmOpen] {
		t.Fatalf("expected alarmOpen in active group")
	}
	if !foundIDs[alarmAck] {
		t.Fatalf("expected alarmAck in active group")
	}
	if foundIDs[alarmRes] {
		t.Fatalf("resolved alarm should NOT be in active group")
	}
	if foundIDs[alarmClosed] {
		t.Fatalf("closed alarm should NOT be in active group")
	}
}
