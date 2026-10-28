package api

import (
	"errors"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/inventory"
)

func TestZoneForDefaultsToUTCAndRefusesUnknownNames(t *testing.T) {
	if loc, err := zoneFor(nil); err != nil || loc != time.UTC {
		t.Fatalf("no zone: %v, %v", loc, err)
	}
	empty := ""
	if loc, err := zoneFor(&empty); err != nil || loc != time.UTC {
		t.Fatalf("empty zone: %v, %v", loc, err)
	}
	ba := "America/Argentina/Buenos_Aires"
	if loc, err := zoneFor(&ba); err != nil || loc.String() != ba {
		t.Fatalf("known zone: %v, %v", loc, err)
	}
	bad := "Mars/Olympus"
	var ve *inventory.ValidationError
	if _, err := zoneFor(&bad); !errors.As(err, &ve) {
		t.Fatalf("unknown zone: error = %v, want a validation error", err)
	}
}
