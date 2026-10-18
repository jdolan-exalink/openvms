//go:build integration

package alarms_test

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
)

// Alarms expose the sub_labels of their events; a registered person's name must be redacted
// there for callers without faces.view (list, detail, event list and lifecycle responses).
func TestAlarmsRedactFaceNamesWithoutFacesView(t *testing.T) {
	c := newCorrEnv(t)
	ctx := context.Background()
	svc := &alarms.Service{Store: c.env.Store}
	tenant := c.cam.TenantID

	res := c.correlate(c.cam, "event", nil, t0, "person")
	err := c.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO face_persons (tenant_id, display_name, frigate_name) VALUES ($1, 'Ana', 'ana')`, tenant); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE events SET sub_labels = ARRAY['ana', 'otro'] WHERE id = (SELECT event_id FROM alarms WHERE id = $1)`, res.AlarmID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	user, _, err := bootstrap.TenantUser(ctx, c.env.Store, tenant, "alarm-op")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []authz.Permission{authz.AlarmsView, authz.AlarmsManage} {
		if _, err := c.env.Svc.CreateGrant(ctx, c.env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: user.UserID, Permission: p, Effect: authz.Allow, ScopeType: authz.ScopeCamera, ScopeID: &c.cam.ID,
		}); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"otro"}
	t.Run("list", func(t *testing.T) {
		page, err := svc.ListPage(ctx, user, alarms.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || !slices.Equal(page.Items[0].EventSubLabels, want) {
			t.Fatalf("list sub_labels = %+v, want %v", page.Items, want)
		}
	})
	t.Run("detail", func(t *testing.T) {
		a, err := svc.Get(ctx, user, res.AlarmID)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(a.EventSubLabels, want) {
			t.Fatalf("detail sub_labels = %v, want %v", a.EventSubLabels, want)
		}
	})
	t.Run("alarm events", func(t *testing.T) {
		evs, err := svc.ListEvents(ctx, user, res.AlarmID)
		if err != nil {
			t.Fatal(err)
		}
		if len(evs) != 1 || !slices.Equal(evs[0].SubLabels, want) {
			t.Fatalf("alarm events sub_labels = %+v, want %v", evs, want)
		}
	})
	t.Run("lifecycle response", func(t *testing.T) {
		a, err := svc.Acknowledge(ctx, user, res.AlarmID)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(a.EventSubLabels, want) {
			t.Fatalf("acknowledge sub_labels = %v, want %v", a.EventSubLabels, want)
		}
	})
	t.Run("faces.view reveals", func(t *testing.T) {
		if _, err := c.env.Svc.CreateGrant(ctx, c.env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: user.UserID, Permission: authz.FacesView, Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenant,
		}); err != nil {
			t.Fatal(err)
		}
		a, err := svc.Get(ctx, user, res.AlarmID)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(a.EventSubLabels, []string{"ana", "otro"}) {
			t.Fatalf("sub_labels = %v, want both labels", a.EventSubLabels)
		}
		evs, err := svc.ListEvents(ctx, user, res.AlarmID)
		if err != nil || len(evs) != 1 || !slices.Equal(evs[0].SubLabels, []string{"ana", "otro"}) {
			t.Fatalf("alarm events = %+v (%v), want both labels", evs, err)
		}
	})
}
