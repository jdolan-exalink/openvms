package media

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestZonesForManifest(t *testing.T) {
	cfg := map[string]any{
		"zones": map[string]any{
			"porch":   map[string]any{"coordinates": "0.1,0.2,0.3,0.4", "objects": []any{"person"}, "inertia": 3},
			"lawn":    map[string]any{"coordinates": []any{"0.1,0.2", "0.3,0.4"}},
			"nocoord": map[string]any{"objects": []any{"car"}},
			"bad":     "not-an-object",
		},
	}
	want := map[string]any{
		"porch": map[string]any{"coordinates": "0.1,0.2,0.3,0.4"},
		"lawn":  map[string]any{"coordinates": []any{"0.1,0.2", "0.3,0.4"}},
	}
	if got := zonesForManifest(cfg); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestZonesForManifestMissing(t *testing.T) {
	for _, cfg := range []map[string]any{nil, {}, {"zones": nil}, {"zones": "x"}, {"zones": map[string]any{"a": map[string]any{}}}} {
		if got := zonesForManifest(cfg); len(got) != 0 {
			t.Fatalf("cfg %#v: expected empty, got %#v", cfg, got)
		}
	}
}

func zoneCams(n int) []zoneCamera {
	cams := make([]zoneCamera, n)
	for i := range cams {
		cams[i] = zoneCamera{id: uuid.New()}
	}
	return cams
}

// Slow cameras are queried concurrently, so the manifest waits for the slowest one,
// not for the sum of all of them.
func TestCollectZonesRunsLookupsConcurrently(t *testing.T) {
	cams := zoneCams(6)
	var inFlight, peak atomic.Int32
	start := time.Now()
	got := collectZones(context.Background(), cams, 3, time.Second, func(ctx context.Context, c zoneCamera) (map[string]any, error) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
		return map[string]any{"z": c.id.String()}, nil
	}, nil)
	if elapsed := time.Since(start); elapsed > 450*time.Millisecond {
		t.Fatalf("6 lookups of 100ms with limit 3 took %v, want ~200ms (concurrent)", elapsed)
	}
	if p := peak.Load(); p > 3 {
		t.Fatalf("peak concurrent lookups = %d, want <= 3", p)
	}
	if len(got) != len(cams) {
		t.Fatalf("zones for %d cameras, want %d", len(got), len(cams))
	}
}

// A camera that fails or hangs is omitted and reported; it never blocks or fails the others.
func TestCollectZonesOmitsFailedAndTimedOutCameras(t *testing.T) {
	cams := zoneCams(3)
	boom := errors.New("frigate down")
	var failed []uuid.UUID
	start := time.Now()
	got := collectZones(context.Background(), cams, 3, 150*time.Millisecond, func(ctx context.Context, c zoneCamera) (map[string]any, error) {
		switch c.id {
		case cams[0].id:
			return nil, boom
		case cams[1].id:
			<-ctx.Done() // hangs until its own timeout
			return nil, ctx.Err()
		}
		return map[string]any{"porch": map[string]any{"coordinates": "0,0,1,1"}}, nil
	}, func(c zoneCamera, err error) { failed = append(failed, c.id) })
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("collectZones took %v with a 150ms per-camera timeout", elapsed)
	}
	if _, ok := got[cams[2].id.String()]; !ok || len(got) != 1 {
		t.Fatalf("zones = %v, want only the healthy camera", got)
	}
	if len(failed) != 2 {
		t.Fatalf("reported failures = %d, want 2", len(failed))
	}
}

// Cameras without zones are left out of the manifest.
func TestCollectZonesSkipsEmptyZones(t *testing.T) {
	cams := zoneCams(2)
	got := collectZones(context.Background(), cams, 2, time.Second, func(ctx context.Context, c zoneCamera) (map[string]any, error) {
		if c.id == cams[0].id {
			return map[string]any{}, nil
		}
		return map[string]any{"lawn": map[string]any{"coordinates": "0,0,1,1"}}, nil
	}, nil)
	if len(got) != 1 {
		t.Fatalf("zones = %v, want only the camera with zones", got)
	}
}
