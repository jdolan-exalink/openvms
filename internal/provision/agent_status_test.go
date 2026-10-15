package provision

import (
	"math"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/agent"
)

func TestApplySnapshotSaturatesUnsignedCounters(t *testing.T) {
	view := &AgentView{}
	applySnapshot(view, agent.Snapshot{
		MemoryTotal: math.MaxUint64, MemoryAvailable: math.MaxUint64,
		CCTVTotal: math.MaxUint64, CCTVFree: math.MaxUint64,
		DatabaseTotal: math.MaxUint64, DatabaseFree: math.MaxUint64,
	})
	for name, got := range map[string]int64{
		"memory total": view.MemoryTotal, "memory available": view.MemoryAvailable,
		"CCTV total": view.CCTVTotal, "CCTV free": view.CCTVFree,
		"database total": view.DatabaseTotal, "database free": view.DatabaseFree,
	} {
		if got != math.MaxInt64 {
			t.Errorf("%s = %d, want %d", name, got, int64(math.MaxInt64))
		}
	}
}

func TestApplySnapshotMapsAIDetectorHardware(t *testing.T) {
	view := &AgentView{}
	applySnapshot(view, agent.Snapshot{
		CoralPCIe: true,
		Hailo:     true,
		Nvidia:    true,
		OpenVINO:  true,
		GPUVendor: "NVIDIA",
		GPUName:   "RTX 4060",
	})
	if !view.Coral || !view.CoralPCIe {
		t.Errorf("expected Coral and CoralPCIe true")
	}
	if !view.Hailo {
		t.Errorf("expected Hailo true")
	}
	if !view.Nvidia {
		t.Errorf("expected Nvidia true")
	}
	if !view.OpenVINO {
		t.Errorf("expected OpenVINO true")
	}
}

func TestApplySnapshotMapsStorageWatchdog(t *testing.T) {
	view := &AgentView{}
	applySnapshot(view, agent.Snapshot{
		StorageBufferPercent: 15,
		StorageWatchdog:      true,
		StorageOver85Percent: true,
		DatabaseCritical:     true,
		PrunedSegmentsCount:  42,
	})
	if view.StorageBufferPercent != 15 || !view.StorageWatchdog || !view.StorageOver85Percent || !view.DatabaseCritical || view.PrunedSegmentsCount != 42 {
		t.Errorf("unexpected storage watchdog mapping: %+v", view)
	}
}

