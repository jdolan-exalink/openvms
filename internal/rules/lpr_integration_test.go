//go:build integration

package rules_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func (f *fixture) lprWatchlist(name, color string, plates ...string) uuid.UUID {
	f.t.Helper()
	var listID uuid.UUID
	actor := authz.Actor{TenantID: &f.env.Demo.TenantID}
	err := f.env.Store.TxRaw(context.Background(), store.ScopeFor(actor), func(tx pgx.Tx) error {
		q := db.New(tx)
		w, err := q.CreateLprWatchlist(context.Background(), db.CreateLprWatchlistParams{
			TenantID:    f.env.Demo.TenantID,
			Name:        name,
			Description: "Test watchlist",
			Color:       color,
			CreatedBy:   &f.env.Admin.UserID,
		})
		if err != nil {
			return err
		}
		listID = w.ID
		for _, p := range plates {
			_, err = q.AddLprWatchlistItem(context.Background(), db.AddLprWatchlistItemParams{
				TenantID:     f.env.Demo.TenantID,
				WatchlistID:  w.ID,
				Plate:        p,
				Notes:        "test item",
				VehicleBrand: "Ford",
				VehicleModel: "Focus",
				VehicleColor: "Black",
				OwnerName:    "Owner",
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		f.t.Fatalf("create lpr watchlist %q: %v", name, err)
	}
	return listID
}

func (f *fixture) lprEvent(plates ...string) rules.EventContext {
	ev := f.eventCtx("car")
	ev.Plates = plates
	err := f.env.Store.TxRaw(context.Background(), store.ScopeFor(authz.Actor{TenantID: &ev.TenantID}), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `
INSERT INTO events (tenant_id, site_id, server_id, camera_id, remote_id, severity, labels, plates, start_time)
VALUES ($1, $2, $3, $4, $5, 'alert', $6, coalesce($7::text[], '{}'), $8) RETURNING id`,
			ev.TenantID, ev.SiteID, ev.ServerID, ev.CameraID, "rules-"+uuid.NewString(), ev.Labels, ev.Plates, ev.Start).Scan(&ev.ID)
	})
	if err != nil {
		f.t.Fatalf("insert lpr test event: %v", err)
	}
	return ev
}

func TestEvaluateEventMatchesLprWatchlists(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	stolenList := f.lprWatchlist("Vehículos Robados", "#dc2626", "AB123CD", "AA000BB")
	rule := f.addRule("Alerta Robo", rules.TriggerEvent, rules.Conditions{
		LprWatchlistIDs: []uuid.UUID{stolenList},
	}, rules.Actions{
		CreateAlarm: true,
		NotifyInApp: true,
	}, true)

	// 1. Events with unrelated plates do not match
	for _, plates := range [][]string{nil, {"XX999YY"}, {"ZZ000ZZ"}} {
		if err := f.svc.EvaluateEvent(ctx, f.lprEvent(plates...)); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.notifications(rule); n != 0 {
		t.Fatalf("events without watchlist plate created %d notifications, want 0", n)
	}

	// 2. Event with matching plate creates notification and alarm
	if err := f.svc.EvaluateEvent(ctx, f.lprEvent("ab123cd")); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(rule); n != 1 {
		t.Fatalf("matching plate created %d notifications, want 1", n)
	}
}

func TestEvaluateEventLprWatchlistCacheExpiration(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	list := f.lprWatchlist("VIP", "#10b981", "VIP001")
	rule := f.addRule("VIP Rule", rules.TriggerEvent, rules.Conditions{
		LprWatchlistIDs: []uuid.UUID{list},
	}, rules.Actions{
		NotifyInApp: true,
	}, true)
	f.svc.Debounce = time.Nanosecond

	// Matches VIP001
	if err := f.svc.EvaluateEvent(ctx, f.lprEvent("VIP001")); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(rule); n != 1 {
		t.Fatalf("VIP001 created %d notifications, want 1", n)
	}

	// Expire cache and add VIP002
	if r, ok := f.svc.Lpr.(*rules.StoreLprResolver); ok {
		r.Expire()
	}
	_ = f.env.Store.TxRaw(ctx, store.ScopeFor(authz.Actor{TenantID: &f.env.Demo.TenantID}), func(tx pgx.Tx) error {
		_, err := db.New(tx).AddLprWatchlistItem(ctx, db.AddLprWatchlistItemParams{
			TenantID:    f.env.Demo.TenantID,
			WatchlistID: list,
			Plate:       "VIP002",
		})
		return err
	})

	time.Sleep(5 * time.Millisecond)
	if err := f.svc.EvaluateEvent(ctx, f.lprEvent("VIP002")); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(rule); n != 2 {
		t.Fatalf("VIP002 after expire created %d notifications, want 2", n)
	}
}
