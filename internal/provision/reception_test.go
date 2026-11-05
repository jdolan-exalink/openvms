package provision

import (
	"testing"
	"time"
)

func TestReceptionStateFollowsTheLastAnalytic(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	cases := []struct {
		name string
		last *time.Time
		err  string
		want ReceptionState
	}{
		{"an analytic arrived a minute ago", ago(time.Minute), "", ReceptionReceiving},
		{"the last one is exactly at the window edge", ago(ReceptionWindow), "", ReceptionReceiving},
		{"the last one is older than the window", ago(ReceptionWindow + time.Second), "", ReceptionIdle},
		{"nothing was ever received", nil, "", ReceptionIdle},
		{"the last ingestion cycle failed, even with a recent analytic", ago(time.Minute), "agent unreachable", ReceptionDown},
		{"the last ingestion cycle failed and nothing was received", nil, "agent unreachable", ReceptionDown},
		{"a blank error is no error", ago(time.Minute), "  ", ReceptionReceiving},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Reception(c.last, c.err, now); got != c.want {
				t.Fatalf("Reception = %q, want %q", got, c.want)
			}
		})
	}
}
