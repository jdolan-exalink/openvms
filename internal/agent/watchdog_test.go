package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnforceStorageSafetyWithinLimits(t *testing.T) {
	tmp := t.TempDir()
	cfg := StorageWatchdogConfig{
		Enabled:             true,
		SafetyBufferPercent: 15,
		RecordingsPath:      tmp,
	}

	// 1000 Total, 300 Free (30% free, well above 15% buffer)
	status, err := EnforceStorageSafety(cfg, 1000, 300, 500, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.OverSafetyThreshold {
		t.Errorf("expected not over threshold, got %+v", status)
	}
	if status.PrunedSegmentsCount != 0 {
		t.Errorf("expected 0 pruned, got %d", status.PrunedSegmentsCount)
	}
	if status.DBCritical {
		t.Errorf("expected DB not critical")
	}
}

func TestEnforceStorageSafetyDisabledDoesNotPrune(t *testing.T) {
	tmp := t.TempDir()
	file1 := filepath.Join(tmp, "segment1.mp4")
	if err := os.WriteFile(file1, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := StorageWatchdogConfig{
		Enabled:             false, // Disabled
		SafetyBufferPercent: 15,
		RecordingsPath:      tmp,
	}

	// 1000 Total, 100 Free (10% free, below 15% safety threshold)
	status, err := EnforceStorageSafety(cfg, 1000, 100, 500, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.OverSafetyThreshold {
		t.Errorf("expected over threshold because free space (10%%) < 15%% buffer")
	}
	if status.PrunedSegmentsCount != 0 {
		t.Errorf("expected 0 pruned because watchdog is disabled, got %d", status.PrunedSegmentsCount)
	}

	// Verify file was NOT deleted
	if _, err := os.Stat(file1); err != nil {
		t.Errorf("file should not have been deleted when watchdog is disabled")
	}
}

func TestEnforceStorageSafetyPrunesOldestWhenOver85(t *testing.T) {
	tmp := t.TempDir()

	// Create 3 files with different modification times
	f1 := filepath.Join(tmp, "2026-10-10_01.mp4")
	f2 := filepath.Join(tmp, "2026-10-11_01.mp4")
	f3 := filepath.Join(tmp, "2026-10-12_01.mp4")

	content := make([]byte, 100)
	if err := os.WriteFile(f1, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f3, content, 0o600); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	_ = os.Chtimes(f1, now.Add(-3*time.Hour), now.Add(-3*time.Hour))
	_ = os.Chtimes(f2, now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	_ = os.Chtimes(f3, now.Add(-1*time.Hour), now.Add(-1*time.Hour))

	cfg := StorageWatchdogConfig{
		Enabled:             true,
		SafetyBufferPercent: 15,
		RecordingsPath:      tmp,
	}

	// 1000 Total, 50 Free (5% free, below 15% buffer). Needs 150 - 50 = 100 bytes freed.
	status, err := EnforceStorageSafety(cfg, 1000, 50, 500, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.OverSafetyThreshold {
		t.Errorf("expected over threshold")
	}
	if status.PrunedSegmentsCount != 1 {
		t.Errorf("expected 1 file pruned to reclaim 100 bytes, got %d", status.PrunedSegmentsCount)
	}

	// Oldest file (f1) should be pruned
	if _, err := os.Stat(f1); !os.IsNotExist(err) {
		t.Errorf("expected oldest file f1 to be pruned")
	}
	// Newer files f2 and f3 should still exist
	if _, err := os.Stat(f2); err != nil {
		t.Errorf("expected f2 to remain")
	}
	if _, err := os.Stat(f3); err != nil {
		t.Errorf("expected f3 to remain")
	}
}

func TestEnforceStorageSafetyDetectsDBCritical(t *testing.T) {
	tmp := t.TempDir()
	cfg := StorageWatchdogConfig{
		Enabled:             true,
		SafetyBufferPercent: 15,
		RecordingsPath:      tmp,
	}

	// DB: 1000 Total, 100 Free (10% free, below 15% threshold)
	status, err := EnforceStorageSafety(cfg, 1000, 300, 1000, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.DBCritical {
		t.Errorf("expected DB to be marked critical when free space < 15%%")
	}
}
