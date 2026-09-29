//go:build integration

package events_test

import (
	"context"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/events"
)

func TestSyncKeepsVMSReview(t *testing.T) {
	env, syncer, svc := setup(t)
	ctx := context.Background()
	syncer.SyncAll(ctx)
	page, err := svc.ListEvents(ctx, env.Admin, events.Filter{Limit: 1})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("list: %v (%d items)", err, len(page.Items))
	}
	id := page.Items[0].ID
	if page.Items[0].Reviewed {
		t.Fatal("the mock Frigate should report the event as not reviewed")
	}
	if _, err := svc.SetReviewed(ctx, env.Admin, id, true); err != nil {
		t.Fatal(err)
	}
	// Frigate still reports it as not reviewed; every later sync must leave the VMS review alone.
	syncer.SyncAll(ctx)
	got, err := svc.GetEvent(ctx, env.Admin, id)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Reviewed {
		t.Fatal("a sync overwrote the VMS review with Frigate's not-reviewed state")
	}
	// A VMS un-review is a deliberate operator action and stays until the next VMS change.
	if _, err := svc.SetReviewed(ctx, env.Admin, id, false); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.GetEvent(ctx, env.Admin, id)
	if got.Reviewed {
		t.Fatal("un-review through the VMS did not stick")
	}
}
