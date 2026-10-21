package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/visionai"
)

type fakeVisionSource struct {
	rows          []visionai.EventRow
	platesVisible bool
}

func (f fakeVisionSource) RecentEvents(context.Context, authz.Actor, time.Duration, time.Time) (visionai.EventSet, error) {
	return visionai.EventSet{Rows: f.rows, PlatesVisible: f.platesVisible}, nil
}

func visionCtx() context.Context {
	tid := uuid.New()
	return WithActor(context.Background(), authz.Actor{UserID: uuid.New(), TenantID: &tid})
}

func TestGetVisionAISlots(t *testing.T) {
	cam := uuid.New()
	now := time.Now()
	h := &Handlers{VisionAI: visionai.NewService(fakeVisionSource{rows: []visionai.EventRow{{
		ID: uuid.New(), CameraID: cam, CameraName: "Gate", Severity: "alert", Labels: []string{"person"},
		Zones: []string{"yard"}, StartTime: now.Add(-5 * time.Second),
	}}})}

	t.Run("fills slots, defaults to general/4 and exposes the contract fields", func(t *testing.T) {
		resp, err := h.GetVisionAISlots(visionCtx(), gen.GetVisionAISlotsRequestObject{})
		if err != nil {
			t.Fatal(err)
		}
		body, ok := resp.(gen.GetVisionAISlots200JSONResponse)
		if !ok {
			t.Fatalf("response type %T", resp)
		}
		if body.Source != gen.VisionAISource("general") || body.Layout != gen.VisionAILayout("4") || len(body.Slots) != 4 {
			t.Fatalf("envelope = %s/%s with %d slots", body.Source, body.Layout, len(body.Slots))
		}
		s := body.Slots[0]
		if s.CameraId == nil || *s.CameraId != cam || deref(s.CameraName) != "Gate" || deref(s.Object) != "person" ||
			deref(s.Zone) != "yard" || deref(s.Alarm) != "high" || deref(s.Confidence) != visionai.DefaultConfidence ||
			deref(s.Score) <= 0 || s.Breakdown == nil || s.AssignedAt == nil || deref(s.EventId) == "" {
			t.Fatalf("filled slot incomplete: %+v", s)
		}
		empty := body.Slots[1]
		if empty.CameraId != nil || empty.Slot != 1 || empty.CameraName != nil || empty.Breakdown != nil {
			t.Fatalf("empty slot must only carry slot/pinned: %+v", empty)
		}
	})

	t.Run("reports when the detection started", func(t *testing.T) {
		resp, err := h.GetVisionAISlots(visionCtx(), gen.GetVisionAISlotsRequestObject{})
		if err != nil {
			t.Fatal(err)
		}
		s := resp.(gen.GetVisionAISlots200JSONResponse).Slots[0]
		if s.DetectedAt == nil || !s.DetectedAt.Equal(now.Add(-5*time.Second)) {
			t.Fatalf("detected_at = %v, want the event start %v", s.DetectedAt, now.Add(-5*time.Second))
		}
	})

	t.Run("applies the inactivity timeout and rejects out-of-range values", func(t *testing.T) {
		ended := now.Add(-20 * time.Second)
		h := &Handlers{VisionAI: visionai.NewService(fakeVisionSource{rows: []visionai.EventRow{{
			ID: uuid.New(), CameraID: cam, CameraName: "Gate", Severity: "detection", Labels: []string{"person"},
			StartTime: now.Add(-time.Minute), EndTime: &ended,
		}}})}
		ten := 10
		resp, err := h.GetVisionAISlots(visionCtx(), gen.GetVisionAISlotsRequestObject{Params: gen.GetVisionAISlotsParams{Inactivity: &ten}})
		if err != nil {
			t.Fatal(err)
		}
		if resp.(gen.GetVisionAISlots200JSONResponse).Slots[0].CameraId != nil {
			t.Fatal("inactivity=10 must hide a camera inactive for 20s")
		}
		bad := 601
		_, err = h.GetVisionAISlots(visionCtx(), gen.GetVisionAISlotsRequestObject{Params: gen.GetVisionAISlotsParams{Inactivity: &bad}})
		if status, _, _ := statusFor(err); status != http.StatusBadRequest {
			t.Fatalf("inactivity=601: status %d (err %v), want 400", status, err)
		}
	})

	t.Run("invalid source maps to 400", func(t *testing.T) {
		bad := gen.VisionAISource("nope")
		_, err := h.GetVisionAISlots(visionCtx(), gen.GetVisionAISlotsRequestObject{Params: gen.GetVisionAISlotsParams{Source: &bad}})
		status, code, _ := statusFor(err)
		if status != http.StatusBadRequest || code != "invalid" {
			t.Fatalf("status/code = %d/%s (err %v)", status, code, err)
		}
	})

	t.Run("missing actor is unauthenticated", func(t *testing.T) {
		_, err := h.GetVisionAISlots(context.Background(), gen.GetVisionAISlotsRequestObject{})
		if status, _, _ := statusFor(err); status != http.StatusUnauthorized {
			t.Fatalf("status = %d, err %v", status, err)
		}
	})

	t.Run("unconfigured service reports unavailable", func(t *testing.T) {
		_, err := (&Handlers{}).GetVisionAISlots(visionCtx(), gen.GetVisionAISlotsRequestObject{})
		if status, _, _ := statusFor(err); status != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, err %v", status, err)
		}
	})

	t.Run("plates_visible mirrors the source signal", func(t *testing.T) {
		for _, visible := range []bool{true, false} {
			hh := &Handlers{VisionAI: visionai.NewService(fakeVisionSource{platesVisible: visible})}
			resp, err := hh.GetVisionAISlots(visionCtx(), gen.GetVisionAISlotsRequestObject{})
			if err != nil {
				t.Fatal(err)
			}
			if got := resp.(gen.GetVisionAISlots200JSONResponse).PlatesVisible; got != visible {
				t.Fatalf("plates_visible = %v, want %v", got, visible)
			}
		}
	})
}
