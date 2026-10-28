package media

import (
	"context"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/provisionnvr/nvmstest"
)

func TestRecorderDaysAreCountedInTheCallersTimeZone(t *testing.T) {
	srv := nvmstest.New(t, "admin", recorderPass)
	n, sealer := playbackVideo(t, newFakeG2(t, 200))
	cam := recorderAt(t, sealer, srv)
	s := &Service{NVR: n}
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 28, 23, 59, 0, 0, time.UTC)

	days, err := s.nvrDays(context.Background(), cam, from, to, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || days[0] != "2026-10-28" {
		t.Fatalf("days = %v, want only 2026-10-28 (the fixture's segments are on that UTC day)", days)
	}
	// 10:27 UTC is 07:27 in Buenos Aires: the same date. The fixture's later segments stay that day too.
	ba, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Skip("no tz database")
	}
	days, err = s.nvrDays(context.Background(), cam, from, to, ba)
	if err != nil || len(days) != 1 || days[0] != "2026-10-28" {
		t.Fatalf("days in Buenos Aires = %v, %v", days, err)
	}
	// A zone far ahead pushes late-evening UTC segments to the next date.
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	days, err = s.nvrDays(context.Background(), cam, from, to, tokyo)
	if err != nil || len(days) == 0 {
		t.Fatalf("days in Tokyo = %v, %v", days, err)
	}
}

func TestRecordingDaysAreCachedPerKey(t *testing.T) {
	s := &Service{}
	calls := 0
	load := func() ([]string, error) { calls++; return []string{"2026-10-28"}, nil }
	for range 3 {
		if _, err := s.cachedDays("k", load); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.cachedDays("other", load); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("loads = %d, want 2 (one per key)", calls)
	}
	s.days["k"] = recordingDaysEntry{days: nil, expires: time.Now().Add(-time.Second)}
	if _, err := s.cachedDays("k", load); err != nil || calls != 3 {
		t.Fatalf("an expired entry must be reloaded: calls=%d err=%v", calls, err)
	}
}

func TestAFailedLoadIsNotCached(t *testing.T) {
	s := &Service{}
	calls := 0
	fail := func() ([]string, error) { calls++; return nil, context.DeadlineExceeded }
	for range 2 {
		if _, err := s.cachedDays("k", fail); err == nil {
			t.Fatal("the failure must reach the caller")
		}
	}
	if calls != 2 {
		t.Fatalf("loads = %d, want 2: a failure must not be remembered", calls)
	}
}
