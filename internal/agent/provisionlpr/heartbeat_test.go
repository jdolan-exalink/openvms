package provisionlpr

import (
	"testing"
	"time"
)

func TestHeartbeatManagerTracksOnlineAndOffline(t *testing.T) {
	hm := NewHeartbeatManager(30 * time.Second)
	t0 := time.Now()

	// Initial check: not found
	_, found := hm.Status("cam-01", "10.0.15.107", t0)
	if found {
		t.Fatal("expected camera not to exist yet")
	}

	// Keepalive received
	hm.RecordKeepalive("cam-01", "10.0.15.107", t0)
	status, found := hm.Status("cam-01", "10.0.15.107", t0)
	if !found || !status.Online || status.KeepaliveCount != 1 {
		t.Fatalf("unexpected status after keepalive: %+v", status)
	}

	// 10 seconds later: still online
	status, _ = hm.Status("cam-01", "10.0.15.107", t0.Add(10*time.Second))
	if !status.Online {
		t.Fatal("expected camera to be online 10s after keepalive")
	}

	// 40 seconds later (> 30s timeout): marked offline
	status, _ = hm.Status("cam-01", "10.0.15.107", t0.Add(40*time.Second))
	if status.Online {
		t.Fatal("expected camera to be offline after timeout")
	}

	// New event received: camera returns online
	hm.RecordEvent("cam-01", "10.0.15.107", "AF123BC", t0.Add(45*time.Second), t0.Add(45*time.Second))
	status, _ = hm.Status("cam-01", "10.0.15.107", t0.Add(45*time.Second))
	if !status.Online || status.EventCount != 1 || status.LastPlate != "AF123BC" {
		t.Fatalf("unexpected status after event: %+v", status)
	}

	// AllStatuses
	statuses := hm.AllStatuses(t0.Add(45*time.Second))
	if len(statuses) != 1 || !statuses[0].Online {
		t.Fatalf("unexpected AllStatuses: %+v", statuses)
	}
}
