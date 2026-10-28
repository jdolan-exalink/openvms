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
