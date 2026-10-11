package control

import (
	"math"
	"testing"
)

func TestCount32BoundsGeneratedProtoCounters(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   int
		want int32
	}{
		{name: "negative", in: -1, want: 0},
		{name: "ordinary", in: 42, want: 42},
		{name: "overflow", in: math.MaxInt32 + 1, want: math.MaxInt32},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := count32(tt.in); got != tt.want {
				t.Fatalf("count32(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
