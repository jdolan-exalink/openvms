package mediasession

import (
	"testing"
	"time"
)

func TestSessionManager_Lifecycle(t *testing.T) {
	mgr := NewManager(1 * time.Minute)
	defer mgr.Close()

	// 1. Create
	s := mgr.CreateSession(CreateSessionInput{
		SessionID:      "sess-test-01",
		CameraID:       "cam-1",
		NodeID:         "node-1",
		ClientDeviceID: "dev-desktop",
		Profile:        ProfileSub,
		Transport:      TransportRTPUDP,
		Path:           PathDirectLAN,
	})

	if s.ID != "sess-test-01" || s.State != StateActive || s.Profile != ProfileSub {
		t.Fatalf("unexpected initial session: %+v", s)
	}

	// 2. Adaptive profile change (grid tile -> fullscreen)
	err := mgr.UpdateProfile("sess-test-01", ProfileMain)
	if err != nil {
		t.Fatalf("UpdateProfile failed: %v", err)
	}

	snap, ok := mgr.GetSession("sess-test-01")
	if !ok || snap.Profile != ProfileMain {
		t.Errorf("expected ProfileMain after update, got %+v", snap)
	}

	// 3. Migrate path (LAN unavailable -> fallback to Relay)
	err = mgr.MigratePath("sess-test-01", PathRelay, TransportWebRTC)
	if err != nil {
		t.Fatalf("MigratePath failed: %v", err)
	}

	snap, _ = mgr.GetSession("sess-test-01")
	if snap.Path != PathRelay || snap.Transport != TransportWebRTC {
		t.Errorf("expected PathRelay and TransportWebRTC, got %+v", snap)
	}

	// 4. Telemetry update
	err = mgr.RecordTelemetry("sess-test-01", 10240, 2, 14.5)
	if err != nil {
		t.Fatalf("RecordTelemetry failed: %v", err)
	}

	view := snap.Snapshot()
	if view.BytesSent != 10240 || view.DroppedPackets != 2 || view.RTTMs != 14.5 {
		t.Errorf("telemetry not recorded accurately: %+v", view)
	}

	// 5. Active sessions list
	active := mgr.ListActiveSessions()
	if len(active) != 1 {
		t.Errorf("expected 1 active session, got %d", len(active))
	}

	// 6. Close session
	err = mgr.CloseSession("sess-test-01")
	if err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}

	_, ok = mgr.GetSession("sess-test-01")
	if ok {
		t.Errorf("expected session to be removed from active pool after close")
	}
}

func TestSessionManager_NonExistent(t *testing.T) {
	mgr := NewManager(1 * time.Minute)
	defer mgr.Close()

	if err := mgr.UpdateProfile("invalid", ProfileMain); err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
	if err := mgr.MigratePath("invalid", PathRelay, TransportWebRTC); err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
	if err := mgr.RecordTelemetry("invalid", 100, 0, 1.0); err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
	if err := mgr.CloseSession("invalid"); err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}
