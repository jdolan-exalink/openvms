package visionai

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func serverRow(server, cam uuid.UUID, name string, start time.Time, labels ...string) EventRow {
	r := row(cam, name, start, labels...)
	r.ServerID = server
	return r
}

func TestSlotsFilteredByServers(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	srvA, srvB := uuid.New(), uuid.New()
	camA, camB := uuid.New(), uuid.New()
	ctx := context.Background()
	rows := []EventRow{
		serverRow(srvA, camA, "Camera of A", now.Add(-5*time.Second), "person"),
		serverRow(srvB, camB, "Camera of B", now.Add(-8*time.Second), "person"),
	}
	newSvc := func() *Service {
		svc := NewService(&fakeSource{rows: rows})
		svc.Clock = func() time.Time { return now }
		return svc
	}
	cameras := func(res Result) map[uuid.UUID]bool {
		out := map[uuid.UUID]bool{}
		for _, s := range res.Slots {
			if s.CameraID != uuid.Nil {
				out[s.CameraID] = true
			}
		}
		return out
	}

	t.Run("without a filter every server is shown", func(t *testing.T) {
		res, err := newSvc().Slots(ctx, tenantActor(), "general", "4")
		if err != nil {
			t.Fatal(err)
		}
		if got := cameras(res); !got[camA] || !got[camB] {
			t.Fatalf("cameras = %v", got)
		}
	})

	t.Run("only the selected servers' events fill the slots", func(t *testing.T) {
		res, err := newSvc().Slots(ctx, tenantActor(), "general", "4", WithServers([]uuid.UUID{srvA}))
		if err != nil {
			t.Fatal(err)
		}
		if got := cameras(res); !got[camA] || got[camB] {
			t.Fatalf("cameras = %v, want only A's", got)
		}
	})

	t.Run("several servers can be selected", func(t *testing.T) {
		res, err := newSvc().Slots(ctx, tenantActor(), "general", "4", WithServers([]uuid.UUID{srvB, srvA}))
		if err != nil {
			t.Fatal(err)
		}
		if got := cameras(res); !got[camA] || !got[camB] {
			t.Fatalf("cameras = %v", got)
		}
	})

	t.Run("a filter that matches nothing leaves the slots empty instead of showing everything", func(t *testing.T) {
		res, err := newSvc().Slots(ctx, tenantActor(), "general", "4", WithServers([]uuid.UUID{uuid.New()}))
		if err != nil {
			t.Fatal(err)
		}
		if got := cameras(res); len(got) != 0 {
			t.Fatalf("cameras = %v, want none", got)
		}
	})

	t.Run("an unchecked server's camera does not linger in a slot", func(t *testing.T) {
		svc, actor := newSvc(), tenantActor()
		if res, _ := svc.Slots(ctx, actor, "general", "4"); !cameras(res)[camB] {
			t.Fatalf("B should be in a slot without a filter")
		}
		res, err := svc.Slots(ctx, actor, "general", "4", WithServers([]uuid.UUID{srvA}))
		if err != nil {
			t.Fatal(err)
		}
		if got := cameras(res); got[camB] {
			t.Fatalf("camera of the unchecked server stayed in a slot: %v", got)
		}
		// Going back to everything does not need to remember the filtered session.
		if res, _ := svc.Slots(ctx, actor, "general", "4"); !cameras(res)[camB] {
			t.Fatalf("B should be back once every server is selected")
		}
	})

	t.Run("the order of the selected servers does not matter", func(t *testing.T) {
		svc, actor := newSvc(), tenantActor()
		first, _ := svc.Slots(ctx, actor, "general", "4", WithServers([]uuid.UUID{srvA, srvB}))
		second, _ := svc.Slots(ctx, actor, "general", "4", WithServers([]uuid.UUID{srvB, srvA}))
		if len(svc.sessions) != 1 {
			t.Fatalf("one selection must use one allocator session, got %d", len(svc.sessions))
		}
		if len(first.Slots) != len(second.Slots) {
			t.Fatal("slot count changed")
		}
	})
}
