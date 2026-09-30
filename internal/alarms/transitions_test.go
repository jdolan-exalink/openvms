package alarms_test

import (
	"testing"

	"github.com/jdolan-exalink/openvms/internal/alarms"
)

func TestAlarmTransitionTable(t *testing.T) {
	// Full transition table testing every (from, to) status pair.
	type tc struct {
		from    string
		to      string
		allowed bool
	}

	tests := []tc{
		// From open
		{"open", "open", true},
		{"open", "acknowledged", true},
		{"open", "assigned", true},
		{"open", "investigating", true},
		{"open", "resolved", true},
		{"open", "closed", true},

		// From acknowledged
		{"acknowledged", "open", false},
		{"acknowledged", "acknowledged", true},
		{"acknowledged", "assigned", true},
		{"acknowledged", "investigating", true},
		{"acknowledged", "resolved", true},
		{"acknowledged", "closed", true},

		// From assigned
		{"assigned", "open", false},
		{"assigned", "acknowledged", true},
		{"assigned", "assigned", true},
		{"assigned", "investigating", true},
		{"assigned", "resolved", true},
		{"assigned", "closed", true},

		// From investigating
		{"investigating", "open", false},
		{"investigating", "acknowledged", true},
		{"investigating", "assigned", true},
		{"investigating", "investigating", true},
		{"investigating", "resolved", true},
		{"investigating", "closed", true},

		// From resolved
		{"resolved", "open", false},
		{"resolved", "acknowledged", false},
		{"resolved", "assigned", false},
		{"resolved", "investigating", true},
		{"resolved", "resolved", true},
		{"resolved", "closed", true},

		// From closed (terminal)
		{"closed", "open", false},
		{"closed", "acknowledged", false},
		{"closed", "assigned", false},
		{"closed", "investigating", false},
		{"closed", "resolved", false},
		{"closed", "closed", true},

		// Unknown statuses
		{"unknown", "open", false},
		{"open", "invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.from+"->"+tt.to, func(t *testing.T) {
			got := alarms.CanTransition(tt.from, tt.to)
			if got != tt.allowed {
				t.Fatalf("CanTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.allowed)
			}
		})
	}
}
