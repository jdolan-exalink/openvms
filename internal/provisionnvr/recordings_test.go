package provisionnvr

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/provisionnvr/nvmstest"
)

func recClient(t *testing.T, srv *nvmstest.Server) *Client {
	t.Helper()
	c, err := NewClient(srv.URL, NewCredentials("admin", "pw-ok"), Config{})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRecordLogReturnsTheSegmentsInUTCAndAsksInUTC(t *testing.T) {
	srv := nvmstest.New(t, "admin", "pw-ok")
	var mu sync.Mutex
	var body string
	srv.OnRequest = func(cmd, b string) {
		if cmd == "queryChlRecLog" {
			mu.Lock()
			body = b
			mu.Unlock()
		}
	}
	from := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	segs, err := recClient(t, srv).RecordLog(context.Background(), "00000007-0000-0000-0000-000000000000", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 3 || segs[0].Type != "MOTION" || segs[2].Type != "SCHEDULE" {
		t.Fatalf("segments = %+v", segs)
	}
	if want := time.Date(2026, 10, 28, 10, 27, 41, 0, time.UTC); !segs[1].Start.Equal(want) || segs[1].End.Sub(segs[1].Start) != 37*time.Second {
		t.Fatalf("second segment = %+v, want it to start %v and last 37 s", segs[1], want)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, want := range []string{
		"<startTimeEx>2026-10-28 08:00:00</startTimeEx>", "<endTimeEx>2026-10-28 12:00:00</endTimeEx>",
		"<chl id='{00000007-0000-0000-0000-000000000000}'>", "<modeType>modeOne</modeType>", "<item>NORMALALL</item>", "<item>SCHEDULE</item>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the request lacks %q:\n%s", want, body)
		}
	}
}

func TestRecordLogRefusesBadRangesAndIdsBeforeAskingTheRecorder(t *testing.T) {
	srv := nvmstest.New(t, "admin", "pw-ok")
	c := recClient(t, srv)
	now := time.Now()
	for name, tc := range map[string]struct {
		id       string
		from, to time.Time
	}{
		"empty range":  {"00000001-0000-0000-0000-000000000000", now, now},
		"backwards":    {"00000001-0000-0000-0000-000000000000", now, now.Add(-time.Hour)},
		"over a month": {"00000001-0000-0000-0000-000000000000", now, now.Add(32 * 24 * time.Hour)},
		"empty id":     {"", now, now.Add(time.Hour)},
		"markup in id": {"1'></chl><evil/>", now, now.Add(time.Hour)},
	} {
		if _, err := c.RecordLog(context.Background(), tc.id, tc.from, tc.to); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}
	if len(srv.Calls()) != 0 {
		t.Fatalf("a refused query reached the recorder: %v", srv.Calls())
	}
}

func TestRecordLogIgnoresMalformedSegmentsAndRefusesANonUTCLog(t *testing.T) {
	srv := nvmstest.New(t, "admin", "pw-ok")
	srv.Override["queryChlRecLog"] = `<response><status>success</status><content><recList timeZone="UTC">` +
		`<item><recType>MOTION</recType><startTime>nonsense</startTime><endTime>2026-10-28 10:00:00</endTime></item>` +
		`<item><recType>MOTION</recType><startTime>2026-10-28 10:05:00</startTime><endTime>2026-10-28 10:05:00</endTime></item>` +
		`<item><recType>MOTION</recType><startTime>2026-10-28 10:10:00</startTime><endTime>2026-10-28 10:11:00</endTime></item></recList></content></response>`
	c := recClient(t, srv)
	from := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	segs, err := c.RecordLog(context.Background(), "1", from, from.Add(time.Hour))
	if err != nil || len(segs) != 1 || segs[0].End.Sub(segs[0].Start) != time.Minute {
		t.Fatalf("segments = %+v, %v; want only the valid one", segs, err)
	}
	srv.Override["queryChlRecLog"] = `<response><status>success</status><content><recList timeZone="Local"><item><recType>MOTION</recType><startTime>2026-10-28 10:10:00</startTime><endTime>2026-10-28 10:11:00</endTime></item></recList></content></response>`
	if _, err := c.RecordLog(context.Background(), "1", from, from.Add(time.Hour)); err == nil {
		t.Fatal("a log that is not in UTC would shift every segment and must be refused")
	} else if class, _ := ClassOf(err); class != ClassUnsupported {
		t.Fatalf("class = %q, want unsupported", class)
	}
}

func TestUTCOffsetIsReadFromTheRecordersClock(t *testing.T) {
	srv := nvmstest.New(t, "admin", "pw-ok")
	c := recClient(t, srv)
	// The fixture says it is 28/10/2026 08:40:08 there; that is 11:40 UTC, so the recorder is 3 hours behind.
	off, err := c.utcOffsetAt(context.Background(), time.Date(2026, 10, 28, 11, 40, 11, 0, time.UTC))
	if err != nil || off != -3*time.Hour {
		t.Fatalf("offset = %v, %v; want -3h", off, err)
	}
	srv.Override["queryTimeCfg"] = `<response><status>success</status><content><synchronizeInfo><currentTime>2026/10/28 05:10:03 PM</currentTime></synchronizeInfo><formatInfo><date>year-month-day</date><time>12</time></formatInfo></content></response>`
	off, err = c.utcOffsetAt(context.Background(), time.Date(2026, 10, 28, 11, 40, 0, 0, time.UTC))
	if err != nil || off != 5*time.Hour+30*time.Minute {
		t.Fatalf("12 hour clock: offset = %v, %v; want +5h30m", off, err)
	}
	srv.Override["queryTimeCfg"] = `<response><status>success</status><content><synchronizeInfo><currentTime>garbage</currentTime></synchronizeInfo><formatInfo><date>day-month-year</date><time>24</time></formatInfo></content></response>`
	if _, err := c.utcOffsetAt(context.Background(), time.Now()); err == nil {
		t.Fatal("an unreadable clock must not turn into an offset")
	}
	srv.Override["queryTimeCfg"] = `<response><status>success</status><content><synchronizeInfo><currentTime>28/10/2020 08:40:08</currentTime></synchronizeInfo><formatInfo><date>day-month-year</date><time>24</time></formatInfo></content></response>`
	if _, err := c.utcOffsetAt(context.Background(), time.Date(2026, 10, 28, 11, 40, 0, 0, time.UTC)); err == nil {
		t.Fatal("a clock years away from now is not a time zone and must be refused")
	}
}

func TestReplayPathUsesTheRecordersLocalTime(t *testing.T) {
	// The segment the owner's unit proved: 10:27:41 UTC is 07:27:41 in a recorder three hours behind.
	start := time.Date(2026, 10, 28, 10, 27, 41, 0, time.UTC)
	got := ReplayPath(7, StreamMain, start, -3*time.Hour, 37*time.Second)
	if want := "/chID=7&date=2026-10-28&time=07:27:41&timelen=37&streamType=main&linkType=tcp"; got != want {
		t.Fatalf("ReplayPath = %q, want %q", got, want)
	}
	// Crossing midnight changes the date as well, and a fraction of a second rounds up.
	got = ReplayPath(10, StreamSub, time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC), -3*time.Hour, 1500*time.Millisecond)
	if want := "/chID=10&date=2026-10-28&time=22:00:00&timelen=2&streamType=sub&linkType=tcp"; got != want {
		t.Fatalf("ReplayPath = %q, want %q", got, want)
	}
	if got := ReplayPath(1, StreamMain, start, 0, 0); !strings.Contains(got, "timelen=1&") {
		t.Fatalf("a zero duration must still ask for one second, got %q", got)
	}
}

func TestRecordLogCommandIsReadOnly(t *testing.T) {
	srv := nvmstest.New(t, "admin", "pw-ok")
	from := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	if _, err := recClient(t, srv).RecordLog(context.Background(), "1", from, from.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range srv.Calls() {
		if cmd != "reqLogin" && cmd != "doLogin" && !strings.HasPrefix(cmd, "query") {
			t.Fatalf("sent %q, which is not a login or a query", cmd)
		}
	}
}
