package inventory

import (
	"context"
	"errors"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func countingService(connects *int) *Service {
	return &Service{Connect: func(context.Context, frigate.ConnInfo) (frigate.Adapter, error) {
		*connects++
		return nil, errors.New("must not connect")
	}}
}

func TestAdaptersNeverConnectToAProvisionRecorder(t *testing.T) {
	connects := 0
	_, err := NewAdapters(countingService(&connects)).Get(context.Background(), db.FrigateServer{Kind: KindProvisionNVR})
	var uke *UnsupportedKindError
	if !errors.As(err, &uke) || uke.Kind != KindProvisionNVR {
		t.Fatalf("Get error = %v, want UnsupportedKindError", err)
	}
	if connects != 0 {
		t.Fatalf("Connect was called %d times for a recorder that is not Frigate", connects)
	}
}

func TestFrigateAdapterRefusesOtherKindsButNotFrigate(t *testing.T) {
	connects := 0
	svc := countingService(&connects)
	if _, err := svc.frigateAdapter(context.Background(), db.GetServerRow{Kind: KindProvisionNVR}); err == nil || connects != 0 {
		t.Fatalf("a Provision recorder must be refused before any connection: err=%v connects=%d", err, connects)
	}
	for _, kind := range []string{KindFrigate, ""} {
		if err := requireFrigate(kind); err != nil {
			t.Fatalf("requireFrigate(%q) = %v, want nil", kind, err)
		}
	}
}
