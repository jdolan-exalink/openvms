package events

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/frigate"
)

func ptr(f float64) *float64 { return &f }

func TestStaleReviewVerdict(t *testing.T) {
	start := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	other := errors.New("frigate unreachable")
	cases := []struct {
		name  string
		found *frigate.Review
		err   error
		want  staleVerdict
	}{
		{"closed in frigate", &frigate.Review{EndTime: ptr(unix(start) + 30)}, nil, staleSetFromFrigate},
		{"still open in frigate", &frigate.Review{}, nil, staleLeave},
		{"gone from frigate", nil, fmt.Errorf("get: %w", frigate.ErrNotFound), staleCloseAtLastActivity},
		{"other error", nil, other, staleLeave},
	}
	for _, c := range cases {
		if got := staleReviewVerdict(c.found, c.err); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestLastKnownActivity(t *testing.T) {
	start := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	later := start.Add(90 * time.Second)
	if got := lastKnownActivity(start, nil); !got.Equal(start) {
		t.Errorf("no activity: got %v want start", got)
	}
	if got := lastKnownActivity(start, &later); !got.Equal(later) {
		t.Errorf("later activity: got %v", got)
	}
	earlier := start.Add(-time.Minute)
	if got := lastKnownActivity(start, &earlier); !got.Equal(start) {
		t.Errorf("activity before start must not shrink the event: got %v", got)
	}
}
