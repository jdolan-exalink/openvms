//go:build integration

package alarms_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
)

type alarmPublicationSpy struct{ subjects []string }

func (p *alarmPublicationSpy) Publish(_ context.Context, subject string, _ []byte) error {
	p.subjects = append(p.subjects, subject)
	return nil
}

func TestUserAlarmOperationsEnqueuePublicationsInCallerTransaction(t *testing.T) {
	c := newCorrEnv(t)
	ctx := context.Background()
	tenantID := c.cam.TenantID
	var managerID, assigneeID uuid.UUID
	err := c.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		createUser := func(name string) (uuid.UUID, error) {
			var id uuid.UUID
			err := tx.QueryRow(ctx, `INSERT INTO users (tenant_id, username, display_name, status)
VALUES ($1, $2, $2, 'active') RETURNING id`, tenantID, name).Scan(&id)
			if err != nil {
				return uuid.Nil, err
			}
			for _, permission := range []authz.Permission{authz.AlarmsView, authz.AlarmsManage} {
				if _, err := tx.Exec(ctx, `INSERT INTO permission_grants
(tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id)
VALUES ($1, 'user', $2, $3, 'allow', 'tenant', $1)`, tenantID, id, string(permission)); err != nil {
					return uuid.Nil, err
				}
			}
			return id, nil
		}
		var err error
		if managerID, err = createUser("outbox-manager-" + uuid.NewString()); err != nil {
			return err
		}
		assigneeID, err = createUser("outbox-assignee-" + uuid.NewString())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.Actor{UserID: managerID, Username: "outbox-manager", TenantID: &tenantID}
	spy := &alarmPublicationSpy{}
	svc := &alarms.Service{Store: c.env.Store, Pub: spy}
	eventIDs := map[uuid.UUID]uuid.UUID{}
	newAlarm := func(label string) uuid.UUID {
		t.Helper()
		result := c.correlate(c.cam, "event", nil, t0, label)
		var eventID uuid.UUID
		if err := c.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT event_id FROM alarms WHERE id=$1`, result.AlarmID).Scan(&eventID)
		}); err != nil {
			t.Fatal(err)
		}
		eventIDs[result.AlarmID] = eventID
		return result.AlarmID
	}

	mainID := newAlarm("main")
	if _, err := svc.Acknowledge(ctx, actor, mainID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Acknowledge(ctx, actor, mainID); err != nil { // idempotent no-op
		t.Fatal(err)
	}
	if _, err := svc.Assign(ctx, actor, mainID, assigneeID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Assign(ctx, actor, mainID, assigneeID); err != nil { // same assignee is a no-op
		t.Fatal(err)
	}
	if _, err := svc.Investigate(ctx, actor, mainID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, actor, mainID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Close(ctx, actor, mainID); err != nil {
		t.Fatal(err)
	}
	bulkAckIDs := []uuid.UUID{newAlarm("bulk-ack-a"), newAlarm("bulk-ack-b")}
	if _, err := svc.Bulk(ctx, actor, bulkAckIDs, "acknowledge"); err != nil {
		t.Fatal(err)
	}
	bulkResolveID := newAlarm("bulk-resolve")
	if _, err := svc.Bulk(ctx, actor, []uuid.UUID{bulkResolveID}, "resolve"); err != nil {
		t.Fatal(err)
	}
	if len(spy.subjects) != 0 {
		t.Fatalf("direct publisher calls = %v, want none", spy.subjects)
	}

	type publication struct {
		subject      string
		payload      []byte
		aggregateKey *string
	}
	var rows []publication
	err = c.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		result, err := tx.Query(ctx, `SELECT subject, payload, aggregate_key FROM realtime_publication_outbox`)
		if err != nil {
			return err
		}
		defer result.Close()
		for result.Next() {
			var row publication
			if err := result.Scan(&row.subject, &row.payload, &row.aggregateKey); err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return result.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]int{
		"alarm.acknowledged." + tenantID.String():  3,
		"alarm.assigned." + tenantID.String():      1,
		"alarm.investigating." + tenantID.String(): 1,
		"alarm.resolved." + tenantID.String():      2,
		"alarm.closed." + tenantID.String():        1,
	}
	wantedStatus := map[string]string{
		"alarm.acknowledged." + tenantID.String():  "acknowledged",
		"alarm.assigned." + tenantID.String():      "assigned",
		"alarm.investigating." + tenantID.String(): "investigating",
		"alarm.resolved." + tenantID.String():      "resolved",
		"alarm.closed." + tenantID.String():        "closed",
	}
	if len(rows) != 8 {
		t.Fatalf("outbox rows = %d, want 8 changed transitions", len(rows))
	}
	for _, row := range rows {
		if wanted[row.subject] == 0 {
			t.Errorf("unexpected subject %q", row.subject)
			continue
		}
		wanted[row.subject]--
		var payload map[string]any
		if err := json.Unmarshal(row.payload, &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload) != 7 {
			t.Errorf("payload fields = %v, want the seven alarm announcement fields", payload)
			continue
		}
		idValue, ok := payload["id"].(string)
		if !ok {
			t.Errorf("payload id = %v, want UUID string", payload["id"])
			continue
		}
		alarmID, err := uuid.Parse(idValue)
		if err != nil {
			t.Errorf("payload id %q is not UUID: %v", idValue, err)
			continue
		}
		updatedAt, hasTimestamp := payload["updated_at"].(string)
		if payload["tenant_id"] != tenantID.String() || payload["site_id"] != c.cam.SiteID.String() || payload["camera_id"] != c.cam.ID.String() || payload["event_id"] != eventIDs[alarmID].String() || !hasTimestamp || updatedAt == "" {
			t.Errorf("payload = %v, wrong tenant/event or missing timestamp", payload)
		}
		if payload["status"] != wantedStatus[row.subject] {
			t.Errorf("payload status = %v for %s, want %s", payload["status"], row.subject, wantedStatus[row.subject])
		}
		if row.aggregateKey == nil || *row.aggregateKey != "alarm/"+alarmID.String() {
			t.Errorf("aggregate key = %v, want alarm/%s", row.aggregateKey, alarmID)
		}
	}
	for subject, count := range wanted {
		if count != 0 {
			t.Errorf("subject %q missing %d outbox rows", subject, count)
		}
	}
}
