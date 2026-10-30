package events

import (
	"testing"

	"github.com/google/uuid"
)

func TestPlateCropKeyFormat(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	serverID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	readID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	got := PlateCropKey(tenantID, serverID, readID)
	want := "tenant/11111111-1111-1111-1111-111111111111/plates/22222222-2222-2222-2222-222222222222/33333333-3333-3333-3333-333333333333.jpg"
	if got != want {
		t.Fatalf("PlateCropKey: got %q want %q", got, want)
	}

	snapGot := PlateSnapshotKey(tenantID, serverID, readID)
	snapWant := "tenant/11111111-1111-1111-1111-111111111111/events/22222222-2222-2222-2222-222222222222/33333333-3333-3333-3333-333333333333_snap.jpg"
	if snapGot != snapWant {
		t.Fatalf("PlateSnapshotKey: got %q want %q", snapGot, snapWant)
	}
}
