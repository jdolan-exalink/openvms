package api

import (
	"math"
	"testing"
)

func TestSignedCapacityBytesSaturatesOverflow(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   uint64
		want int64
	}{
		{name: "ordinary", in: 4096, want: 4096},
		{name: "maximum signed", in: math.MaxInt64, want: math.MaxInt64},
		{name: "overflow", in: math.MaxUint64, want: math.MaxInt64},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := signedCapacityBytes(tt.in); got != tt.want {
				t.Fatalf("signedCapacityBytes(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
