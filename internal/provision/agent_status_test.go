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
