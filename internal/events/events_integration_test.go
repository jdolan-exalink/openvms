//go:build integration

package events_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/platform/objectstore"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

type memBlobs struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (b *memBlobs) Put(_ context.Context, key string, body []byte, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.m[key] = body
	return nil
}

func (b *memBlobs) Get(_ context.Context, key string) ([]byte, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.m[key]
	if !ok {
		return nil, "", objectstore.ErrNotFound
	}
	return v, "image/jpeg", nil
}

func setup(t *testing.T) (*demofix.Env, *events.Syncer, *events.Service) {
	t.Helper()
	env := demofix.Setup(t)
	adapters := inventory.NewAdapters(env.Svc)
	blobs := &memBlobs{m: map[string][]byte{}}
	syncer := &events.Syncer{Store: env.Store, Adapters: adapters, Blobs: blobs, Log: pgtest.Discard(), Interval: time.Second, Backfill: 24 * time.Hour, Concurrency: 2}
	svc := &events.Service{Store: env.Store, Blobs: blobs, Adapters: adapters, Log: pgtest.Discard()}
	return env, syncer, svc
}

func TestFederatedEventIndex(t *testing.T) {
	env, syncer, svc := setup(t)
	ctx := context.Background()
	syncer.SyncAll(ctx)

	all, err := svc.ListEvents(ctx, env.Admin, events.Filter{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != 60 {
		t.Fatalf("admin sees %d events, want the 60 seeded in both Frigates", len(all.Items))
	}
	servers := map[string]bool{}
	for _, e := range all.Items {
		servers[e.ServerName] = true
	}
	if len(servers) != 2 {
		t.Errorf("events come from %d servers, want 2", len(servers))
	}

	t.Run("a second pull does not duplicate", func(t *testing.T) {
		syncer.SyncAll(ctx)
		again, err := svc.ListEvents(ctx, env.Admin, events.Filter{Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Items) != 60 {
			t.Fatalf("%d events after a second pull, want 60", len(again.Items))
		}
	})

	t.Run("operator only sees events of its cameras (PRD §129)", func(t *testing.T) {
		op := env.Actor(t, "operador")
		page, err := svc.ListEvents(ctx, op, events.Filter{Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		allowed := map[string]bool{
			env.Cameras["frigate-h01/acceso_norte"].ID.String(): true,
			env.Cameras["frigate-c01/muelle"].ID.String():       true,
		}
		for _, e := range page.Items {
			if !allowed[e.CameraID.String()] {
				t.Fatalf("operator sees an event of camera %s", e.CameraName)
			}
			if len(e.Plates) > 0 {
				t.Errorf("operator has no lpr.view but got plates %v", e.Plates)
			}
		}
		// A direct fetch of a forbidden event is refused.
		for _, e := range all.Items {
			if !allowed[e.CameraID.String()] {
				if _, err := svc.GetEvent(ctx, op, e.ID); !errors.Is(err, access.ErrForbidden) {
					t.Errorf("GetEvent of a forbidden camera = %v, want forbidden", err)
				}
				break
			}
		}
	})

	t.Run("pagination walks every event once", func(t *testing.T) {
		seen := map[string]bool{}
		cursor := ""
		for range 20 {
			page, err := svc.ListEvents(ctx, env.Admin, events.Filter{Limit: 7, Cursor: cursor})
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range page.Items {
				if seen[e.ID.String()] {
					t.Fatalf("event %s returned twice", e.ID)
				}
				seen[e.ID.String()] = true
			}
			if page.Next == "" {
				break
			}
			cursor = page.Next
		}
		if len(seen) != 60 {
			t.Fatalf("pagination returned %d events, want 60", len(seen))
		}
	})

	t.Run("plates are searchable across servers", func(t *testing.T) {
		reads, err := svc.ListPlates(ctx, env.Admin, events.PlateFilter{Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(reads.Items) == 0 {
			t.Fatal("no plate reads indexed from the LPR cameras")
		}
		want := reads.Items[0].Normalized
		found, err := svc.ListPlates(ctx, env.Admin, events.PlateFilter{Plate: want[1:5]})
		if err != nil {
			t.Fatal(err)
		}
		hit := false
		for _, r := range found.Items {
			hit = hit || r.Normalized == want
		}
		if !hit {
			t.Errorf("partial search %q did not find %s", want[1:5], want)
		}
		withPlate, err := svc.ListEvents(ctx, env.Admin, events.Filter{Plate: want})
		if err != nil {
			t.Fatal(err)
		}
		if len(withPlate.Items) == 0 {
			t.Errorf("no event carries plate %s", want)
		}
		op := env.Actor(t, "operador")
		opReads, err := svc.ListPlates(ctx, op, events.PlateFilter{Plate: want})
		if err != nil {
			t.Fatal(err)
		}
		if len(opReads.Items) != 0 {
			t.Error("operator without lpr.search must not find plates")
		}
	})

	t.Run("thumbnails are copied for finished events", func(t *testing.T) {
		b, _, err := svc.Thumbnail(ctx, env.Admin, all.Items[0].ID)
		if err != nil || len(b) == 0 {
			t.Fatalf("thumbnail: %v", err)
		}
	})

	t.Run("an offline Frigate keeps its events and is backfilled", func(t *testing.T) {
		m := env.Mocks["frigate-h01"]
		m.Server.Offline.Store(true)
		// Five reviews happen while the VMS cannot reach Frigate.
		m.Generator.Seed(5, 20*time.Second, time.Now().Add(time.Minute))
		syncer.SyncAll(ctx)
		during, err := svc.ListEvents(ctx, env.Admin, events.Filter{Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(during.Items) != 60 {
			t.Fatalf("%d events while offline, want the 60 already indexed", len(during.Items))
		}
		st, err := svc.ListSyncStatus(ctx, env.Admin)
		if err != nil {
			t.Fatal(err)
		}
		failing := 0
		for _, s := range st {
			if s.LastError != "" {
				failing++
			}
		}
		if failing != 1 {
			t.Errorf("%d servers report a sync error, want 1", failing)
		}

		m.Server.Offline.Store(false)
		syncer.SyncAll(ctx)
		after, err := svc.ListEvents(ctx, env.Admin, events.Filter{Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(after.Items) != 65 {
			t.Fatalf("%d events after reconnecting, want 65 (backfill)", len(after.Items))
		}
	})
}
