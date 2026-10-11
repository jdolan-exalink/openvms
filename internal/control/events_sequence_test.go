package control

import (
	"math"
	"testing"
)

func TestIncrementEventSequenceStopsAtInt64Limit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		current   int64
		want      int64
		wantValid bool
	}{
		{name: "initial", current: 0, want: 1, wantValid: true},
		{name: "last valid", current: math.MaxInt64 - 1, want: math.MaxInt64, wantValid: true},
		{name: "exhausted", current: math.MaxInt64, want: 0},
		{name: "already wrapped", current: math.MinInt64, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, valid := incrementEventSequence(tc.current)
			if got != tc.want || valid != tc.wantValid {
				t.Fatalf("incrementEventSequence(%d) = (%d, %v), want (%d, %v)", tc.current, got, valid, tc.want, tc.wantValid)
			}
		})
	}
}
