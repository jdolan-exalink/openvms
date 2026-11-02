package agent

import (
	"context"
	"log/slog"
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

func writeAged(t *testing.T, dir, name string, age time.Duration, size int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
	return p
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// A metrics request must never walk or delete the recordings, however full the disk is: that walk
// took minutes on a recorder with millions of segments, hung the agent's API and grew its memory.
func TestReadNeverPrunesEvenWhenTheDiskIsOverTheThreshold(t *testing.T) {
	dir := t.TempDir()
	old := writeAged(t, dir, "old.mp4", 48*time.Hour, 100)
	prev := diskUsageFn
	diskUsageFn = func(string) (uint64, uint64) { return 1000, 100 } // 10% free, below the 15% buffer
	t.Cleanup(func() { diskUsageFn = prev })

	snap := Read(Paths{CCTV: dir, DB: t.TempDir()}, "test")

	if !snap.StorageOver85Percent {
		t.Fatalf("the snapshot must still report the disk as over the threshold: %+v", snap)
	}
	if snap.PrunedSegmentsCount != 0 || !exists(old) {
		t.Fatalf("Read deleted recordings: pruned=%d exists=%v", snap.PrunedSegmentsCount, exists(old))
	}
}

func TestStorageSafetyStatusOnlyReports(t *testing.T) {
	dir := t.TempDir()
	f := writeAged(t, dir, "a.mp4", time.Hour, 10)
	st := StorageSafetyStatus(StorageWatchdogConfig{Enabled: true, RecordingsPath: dir}, 1000, 50, 500, 400)
	if !st.OverSafetyThreshold || st.PrunedSegmentsCount != 0 || !exists(f) {
		t.Fatalf("status %+v, file exists %v", st, exists(f))
	}
}

func TestPruneKeepsOnlyABoundedSetOfCandidatesAndRemovesTheOldestOfThem(t *testing.T) {
	dir := t.TempDir()
	var files []string
	for i := 0; i < 6; i++ { // a0 is the oldest, a5 the newest
		files = append(files, writeAged(t, dir, "a"+string(rune('0'+i))+".mp4", time.Duration(6-i)*time.Hour, 100))
	}
	prev := maxPruneCandidates
	maxPruneCandidates = 3
	t.Cleanup(func() { maxPruneCandidates = prev })

	count, _, err := pruneOldestRecordings(context.Background(), dir, 10_000) // asks for more than all of it
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("removed %d files, want the 3 oldest only", count)
	}
	for i, f := range files {
		if want := i >= 3; exists(f) != want {
			t.Errorf("%s exists=%v, want %v", filepath.Base(f), exists(f), want)
		}
	}
}

func TestPruneRemovesNothingWhenTheWalkIsCancelled(t *testing.T) {
	dir := t.TempDir()
	f := writeAged(t, dir, "a.mp4", time.Hour, 100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := pruneOldestRecordings(ctx, dir, 10); err == nil {
		t.Fatal("a cancelled walk must report an error")
	}
	if !exists(f) {
		t.Fatal("an unfinished walk may have missed older files and must delete nothing")
	}
}

func TestStoragePrunerRunsInTheBackgroundAndFreesTheOldestSegments(t *testing.T) {
	dir := t.TempDir()
	oldest := writeAged(t, dir, "oldest.mp4", 72*time.Hour, 100)
	newer := writeAged(t, dir, "newer.mp4", time.Hour, 100)
	prev := diskUsageFn
	diskUsageFn = func(string) (uint64, uint64) { return 1000, 120 } // needs 30 more bytes: one segment
	t.Cleanup(func() { diskUsageFn = prev })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunStoragePruner(ctx, Paths{CCTV: dir}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for exists(oldest) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if exists(oldest) || !exists(newer) {
		t.Fatalf("oldest exists=%v newer exists=%v, want only the oldest removed", exists(oldest), exists(newer))
	}
}
