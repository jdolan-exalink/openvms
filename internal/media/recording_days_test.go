package media

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
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
		if _, err := s.cachedDays(context.Background(), "k", load); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.cachedDays(context.Background(), "other", load); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("loads = %d, want 2 (one per key)", calls)
	}
	s.days["k"] = recordingDaysEntry{days: nil, expires: time.Now().Add(-time.Second)}
	if _, err := s.cachedDays(context.Background(), "k", load); err != nil || calls != 3 {
		t.Fatalf("an expired entry must be reloaded: calls=%d err=%v", calls, err)
	}
}

func TestAFailedLoadIsNotCached(t *testing.T) {
	s := &Service{}
	calls := 0
	fail := func() ([]string, error) { calls++; return nil, context.DeadlineExceeded }
	for range 2 {
		if _, err := s.cachedDays(context.Background(), "k", fail); err == nil {
			t.Fatal("the failure must reach the caller")
		}
	}
	if calls != 2 {
		t.Fatalf("loads = %d, want 2: a failure must not be remembered", calls)
	}
}

func TestConcurrentLoadsOfOneKeyShareASingleCall(t *testing.T) {
	s := &Service{}
	var calls atomic.Int32
	release := make(chan struct{})
	load := func() ([]string, error) {
		calls.Add(1)
		<-release
		return []string{"2026-10-28"}, nil
	}
	var wg sync.WaitGroup
	results := make([][]string, 5)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := s.cachedDays(context.Background(), "k", load)
			if err != nil {
				t.Error(err)
			}
			results[i] = d
		}()
	}
	for calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // let the others queue behind the first
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("loads = %d, want 1 shared by all five callers", calls.Load())
	}
	for i, r := range results {
		if len(r) != 1 || r[0] != "2026-10-28" {
			t.Fatalf("caller %d got %v", i, r)
		}
	}
}

func TestAWaitingCallerGivesUpWhenItsContextEnds(t *testing.T) {
	s := &Service{}
	release := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_, _ = s.cachedDays(context.Background(), "k", func() ([]string, error) {
			close(started)
			<-release
			return nil, nil
		})
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := s.cachedDays(ctx, "k", func() ([]string, error) { return nil, nil }); err == nil {
		t.Fatal("a waiter whose context ended must return its error")
	}
	close(release)
}

func TestTheCacheStaysBounded(t *testing.T) {
	s := &Service{}
	for i := 0; i < maxRecordingDaysEntries*3; i++ {
		if _, err := s.cachedDays(context.Background(), fmt.Sprintf("k%d", i), func() ([]string, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.days) > maxRecordingDaysEntries {
		t.Fatalf("entries = %d, want at most %d", len(s.days), maxRecordingDaysEntries)
	}
}

func TestDaysWithinKeepsOnlyTheAskedWindow(t *testing.T) {
	from := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	got := daysWithin([]string{"2026-10-09", "2026-10-10", "2026-10-15", "2026-10-20", "2026-10-21"}, from, to, time.UTC)
	if len(got) != 3 || got[0] != "2026-10-10" || got[2] != "2026-10-20" {
		t.Fatalf("days = %v", got)
	}
}

func TestAStretchEndingAtMidnightDoesNotMarkTheNextDay(t *testing.T) {
	srv := nvmstest.New(t, "admin", recorderPass)
	srv.Override["queryChlRecLog"] = `<response><status>success</status><content><recList timeZone="UTC">` +
		`<item><recType>MOTION</recType><startTime>2026-10-28 23:59:00</startTime><endTime>2026-10-29 00:00:00</endTime></item>` +
		`</recList></content></response>`
	n, sealer := playbackVideo(t, newFakeG2(t, 200))
	cam := recorderAt(t, sealer, srv)
	s := &Service{NVR: n}
	from := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	days, err := s.nvrDays(context.Background(), cam, from, to, time.UTC)
	if err != nil || len(days) != 1 || days[0] != "2026-10-28" {
		t.Fatalf("days = %v, %v; want only 2026-10-28", days, err)
	}
}

func TestRecordingDaysRefusesABadRangeBeforeAnythingElse(t *testing.T) {
	s := &Service{}
	now := time.Now()
	for name, tc := range map[string]struct{ from, to time.Time }{
		"empty":     {now, now},
		"backwards": {now, now.Add(-time.Hour)},
		"too long":  {now, now.Add(63 * 24 * time.Hour)},
	} {
		_, err := s.RecordingDays(context.Background(), authz.Actor{}, uuid.New(), tc.from, tc.to, time.UTC)
		var ve *inventory.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("%s: error = %v, want a validation error", name, err)
		}
	}
}
