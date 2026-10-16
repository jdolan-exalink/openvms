package alarms_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/alarms"
)

func TestCorrelationKey(t *testing.T) {
	cam := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	rule := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	tests := []struct {
		name   string
		source string
		rule   *uuid.UUID
		labels []string
		want   string
	}{
		{"event source, single label", "event", nil, []string{"person"}, "event|none|" + cam.String() + "|person"},
		{"rule source carries the rule id", "rule", &rule, []string{"car"}, "rule|" + rule.String() + "|" + cam.String() + "|car"},
		{"multi label: smallest label, order independent", "event", nil, []string{"person", "car"}, "event|none|" + cam.String() + "|car"},
		{"labels are lower-cased and trimmed", "event", nil, []string{" Person "}, "event|none|" + cam.String() + "|person"},
		{"no label", "event", nil, nil, "event|none|" + cam.String() + "|none"},
		{"blank labels ignored", "event", nil, []string{"", "  "}, "event|none|" + cam.String() + "|none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := alarms.CorrelationKey(tt.source, tt.rule, cam, tt.labels); got != tt.want {
				t.Fatalf("CorrelationKey = %q, want %q", got, tt.want)
			}
		})
	}
}
