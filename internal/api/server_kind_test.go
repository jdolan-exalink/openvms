package api

import (
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func TestToServerReportsItsKind(t *testing.T) {
	for _, kind := range []string{"frigate", "provision_nvr"} {
		v := inventory.ServerView{GetServerRow: db.GetServerRow{Kind: kind}}
		if got := toServer(v).Kind; got != gen.ServerKind(kind) {
			t.Fatalf("toServer kind = %q, want %q", got, kind)
		}
	}
}

func TestStatusForUnsupportedServerKind(t *testing.T) {
	status, code, _ := statusFor(&inventory.UnsupportedKindError{Kind: "provision_nvr"})
	if status != 409 || code != "unsupported_for_server_kind" {
		t.Fatalf("statusFor = %d %q, want 409 unsupported_for_server_kind", status, code)
	}
}
