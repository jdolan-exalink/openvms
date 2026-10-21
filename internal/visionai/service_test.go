package visionai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

type fakeSource struct {
	rows          []EventRow
	platesVisible bool
	err           error
	calls         int
}

func (f *fakeSource) RecentEvents(context.Context, authz.Actor, time.Duration, time.Time) (EventSet, error) {
	f.calls++
	return EventSet{Rows: f.rows, PlatesVisible: f.platesVisible}, f.err
}

func tenantActor() authz.Actor {
	tid := uuid.New()
	return authz.Actor{UserID: uuid.New(), TenantID: &tid}
}

func row(cam uuid.UUID, name string, start time.Time, labels ...string) EventRow {
	return EventRow{ID: uuid.New(), CameraID: cam, CameraName: name, Severity: "detection", Labels: labels, StartTime: start}
}

func TestServiceSlots(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	cam1, cam2 := uuid.New(), uuid.New()
	ctx := context.Background()

	newSvc := func(src *fakeSource) (*Service, *time.Time) {
		clock := now
		svc := NewService(src)
		svc.Clock = func() time.Time { return clock }
		return svc, &clock
	}

	t.Run("reports plate visibility from the source", func(t *testing.T) {
		for _, visible := range []bool{true, false} {
			svc, _ := newSvc(&fakeSource{platesVisible: visible})
			res, err := svc.Slots(ctx, tenantActor(), "lpr", "4")
			if err != nil {
				t.Fatal(err)
			}
			if res.PlatesVisible != visible {
				t.Fatalf("PlatesVisible = %v, want %v", res.PlatesVisible, visible)
			}
		}
	})

	t.Run("ranks real events into slots, person above car", func(t *testing.T) {
		src := &fakeSource{rows: []EventRow{
			row(cam1, "Car cam", now.Add(-10*time.Second), "car"),
			row(cam2, "Person cam", now.Add(-5*time.Second), "person"),
		}}
		svc, _ := newSvc(src)
		res, err := svc.Slots(ctx, tenantActor(), "general", "4")
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Slots) != 4 || res.Slots[0].CameraID != cam2 || res.Slots[1].CameraID != cam1 {
			t.Fatalf("unexpected slots: %+v", res.Slots)
		}
		if res.Source != "general" || res.Layout != "4" || !res.GeneratedAt.Equal(now) {
			t.Fatalf("unexpected envelope: %+v", res)
		}
	})

	t.Run("source preset filters events", func(t *testing.T) {
		src := &fakeSource{rows: []EventRow{row(cam1, "Car cam", now, "car"), row(cam2, "Person cam", now, "person")}}
		svc, _ := newSvc(src)
		res, err := svc.Slots(ctx, tenantActor(), "personas", "4")
		if err != nil {
			t.Fatal(err)
		}
		if res.Slots[0].CameraID != cam2 || res.Slots[1].CameraID != uuid.Nil {
			t.Fatalf("personas must only show the person camera: %+v", res.Slots)
		}
	})

	t.Run("same actor keeps allocator state across calls; other actors are isolated", func(t *testing.T) {
		src := &fakeSource{rows: []EventRow{row(cam1, "A", now, "person")}}
		svc, clock := newSvc(src)
		a := tenantActor()
		// Without an inactivity timeout the slot is protected until max display passes.
		if _, err := svc.Slots(ctx, a, "general", "4", WithInactivity(0)); err != nil {
			t.Fatal(err)
		}
		src.rows = nil
		*clock = now.Add(10 * time.Second)
		res, _ := svc.Slots(ctx, a, "general", "4", WithInactivity(0))
		if res.Slots[0].CameraID != cam1 {
			t.Fatalf("session state lost between calls")
		}
		other, _ := svc.Slots(ctx, tenantActor(), "general", "4", WithInactivity(0))
		if other.Slots[0].CameraID != uuid.Nil {
			t.Fatalf("session leaked across actors")
		}
		if svc.SessionCount() != 2 {
			t.Fatalf("sessions = %d, want 2", svc.SessionCount())
		}
	})

	t.Run("idle sessions are evicted on access", func(t *testing.T) {
		src := &fakeSource{rows: []EventRow{row(cam1, "A", now, "person")}}
		svc, clock := newSvc(src)
		a := tenantActor()
		_, _ = svc.Slots(ctx, a, "general", "4")
		*clock = now.Add(SessionIdleTTL + time.Second)
		_, _ = svc.Slots(ctx, tenantActor(), "general", "4")
		if svc.SessionCount() != 1 {
			t.Fatalf("sessions = %d, want idle one evicted", svc.SessionCount())
		}
	})

	t.Run("validates source and layout before touching the source", func(t *testing.T) {
		src := &fakeSource{}
		svc, _ := newSvc(src)
		var ve *ValidationError
		if _, err := svc.Slots(ctx, tenantActor(), "bogus", "4"); !errors.As(err, &ve) {
			t.Fatalf("err = %v", err)
		}
		if src.calls != 0 {
			t.Fatalf("source queried despite invalid input")
		}
	})

	t.Run("source errors propagate", func(t *testing.T) {
		boom := errors.New("boom")
		svc, _ := newSvc(&fakeSource{err: boom})
		if _, err := svc.Slots(ctx, tenantActor(), "general", "4"); !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("platform actors get their own session scope", func(t *testing.T) {
		src := &fakeSource{rows: []EventRow{row(cam1, "A", now, "person")}}
		svc, _ := newSvc(src)
		if _, err := svc.Slots(ctx, authz.Actor{UserID: uuid.New()}, "general", "1"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestServiceSlotsInactivityOption(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	ctx := context.Background()
	cam := uuid.New()
	ended := now.Add(-20 * time.Second)
	r := row(cam, "A", now.Add(-time.Minute), "person")
	r.EndTime = &ended
	svc := NewService(&fakeSource{rows: []EventRow{r}})
	svc.Clock = func() time.Time { return now }
	a := tenantActor()

	for _, bad := range []int{-1, MaxInactivitySeconds + 1} {
		var verr *ValidationError
		if _, err := svc.Slots(ctx, a, "general", "4", WithInactivity(bad)); !errors.As(err, &verr) {
			t.Fatalf("inactivity %d: err = %v, want a ValidationError", bad, err)
		}
	}
	// The event ended 20s ago: hidden with the 10s timeout, shown with the 30s default.
	short, err := svc.Slots(ctx, a, "general", "4", WithInactivity(10))
	if err != nil {
		t.Fatal(err)
	}
	if short.Slots[0].CameraID != uuid.Nil {
		t.Fatal("10s timeout must hide an event inactive for 20s")
	}
	def, _ := svc.Slots(ctx, a, "general", "4")
	if def.Slots[0].CameraID != cam {
		t.Fatal("the 30s default must still show an event inactive for 20s")
	}
}
