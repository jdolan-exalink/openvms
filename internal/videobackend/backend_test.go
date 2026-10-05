package videobackend

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/frigate"
)

type mockFrigateAdapter struct {
	frigate.Adapter
}

func (m *mockFrigateAdapter) Name() string {
	return "mock"
}

func (m *mockFrigateAdapter) Version() string {
	return "0.14.1"
}

func (m *mockFrigateAdapter) ListCameras(ctx context.Context) ([]frigate.Camera, error) {
	return []frigate.Camera{
		{
			Name:       "front_gate",
			Enabled:    true,
			Zones:      []string{"driveway", "gate"},
			LPR:        true,
			LiveStream: "front_gate_sub",
			HQStream:   "front_gate_main",
		},
	}, nil
}

func (m *mockFrigateAdapter) Recordings(ctx context.Context, camera string, after, before float64) ([]frigate.RecordingSegment, error) {
	return []frigate.RecordingSegment{
		{
			StartTime: after,
			EndTime:   after + 60,
			Motion:    10,
			Objects:   2,
		},
	}, nil
}

func (m *mockFrigateAdapter) Reviews(ctx context.Context, q frigate.ReviewQuery) ([]frigate.Review, error) {
	end := q.After + 15
	return []frigate.Review{
		{
			ID:        "rev-1",
			Camera:    "front_gate",
			StartTime: q.After,
			EndTime:   &end,
			Data: frigate.ReviewData{
				Objects: []string{"car"},
				Zones:   []string{"driveway"},
			},
		},
	}, nil
}

func (m *mockFrigateAdapter) Capabilities(ctx context.Context) (frigate.Capabilities, error) {
	return frigate.Capabilities{
		PTZ:             true,
		Audio:           true,
		LPR:             true,
		FaceRecognition: false,
	}, nil
}

func TestFrigateBackend_ListCameras(t *testing.T) {
	adapter := &mockFrigateAdapter{}
	backend := NewFrigateBackend(adapter, "http://192.168.1.100:5000")

	cams, err := backend.ListCameras(context.Background())
	if err != nil {
		t.Fatalf("ListCameras failed: %v", err)
	}

	if len(cams) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(cams))
	}

	cam := cams[0]
	if cam.Name != "front_gate" || !cam.LPR || !cam.Enabled {
		t.Errorf("unexpected camera data: %+v", cam)
	}
	if cam.MainStream != "front_gate_main" || cam.SubStream != "front_gate_sub" {
		t.Errorf("unexpected stream mappings: main=%s sub=%s", cam.MainStream, cam.SubStream)
	}
}

func TestFrigateBackend_GetLiveStream(t *testing.T) {
	adapter := &mockFrigateAdapter{}
	backend := NewFrigateBackend(adapter, "http://node01.lan:5000")

	mainStream, err := backend.GetLiveStream(context.Background(), "front_gate", ProfileMain)
	if err != nil {
		t.Fatalf("GetLiveStream failed: %v", err)
	}
	if mainStream.Width != 1920 || mainStream.Height != 1080 {
		t.Errorf("expected 1920x1080 for main profile, got %dx%d", mainStream.Width, mainStream.Height)
	}

	subStream, err := backend.GetLiveStream(context.Background(), "front_gate", ProfileSub)
	if err != nil {
		t.Fatalf("GetLiveStream sub failed: %v", err)
	}
	if subStream.Width != 640 || subStream.Height != 360 {
		t.Errorf("expected 640x360 for sub profile, got %dx%d", subStream.Width, subStream.Height)
	}
}

func TestFrigateBackend_GetRecording(t *testing.T) {
	adapter := &mockFrigateAdapter{}
	backend := NewFrigateBackend(adapter, "")

	start := time.Unix(1700000000, 0)
	end := time.Unix(1700000300, 0)

	segs, err := backend.GetRecording(context.Background(), "front_gate", start, end)
	if err != nil {
		t.Fatalf("GetRecording failed: %v", err)
	}

	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}

	if segs[0].Duration != 60 {
		t.Errorf("expected 60s duration, got %f", segs[0].Duration)
	}
}

func TestFrigateBackend_GetEvents(t *testing.T) {
	adapter := &mockFrigateAdapter{}
	backend := NewFrigateBackend(adapter, "")

	events, err := backend.GetEvents(context.Background(), EventFilter{
		CameraID: "front_gate",
		After:    time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Label != "car" {
		t.Errorf("expected label car, got %s", events[0].Label)
	}
}

func TestFrigateBackend_GetCameraCapabilities(t *testing.T) {
	adapter := &mockFrigateAdapter{}
	backend := NewFrigateBackend(adapter, "")

	caps, err := backend.GetCameraCapabilities(context.Background(), "front_gate")
	if err != nil {
		t.Fatalf("GetCameraCapabilities failed: %v", err)
	}

	if !caps.PTZ || !caps.AudioIn || !caps.LPR {
		t.Errorf("expected PTZ, AudioIn, and LPR enabled, got %+v", caps)
	}
}

func init() {
	_ = http.StatusOK
}
