//go:build integration

package media_test

import (
	"context"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// A saved view keeps its virtual tiles (detection channels, Vision AI sources) through storage,
// for its owner and for another user reading the shared view.
func TestViewKeepsVisionTiles(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	owner := env.Actor(t, "operador")
	viewer := env.Actor(t, "supervisor")
	for _, p := range []authz.Permission{authz.ViewsCreateShared, authz.ViewsCreatePrivate} {
		if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: owner.UserID, Permission: p,
			Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &env.Demo.TenantID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	svc := &media.Service{Store: env.Store, Adapters: inventory.NewAdapters(env.Svc), Log: pgtest.Discard()}
	created, err := svc.CreateView(ctx, owner, media.ViewInput{Name: "Totem", Shared: true, Layout: media.Layout{Columns: 2, Cells: []*media.Cell{
		{Vision: &media.VisionRef{ID: "visionai:lpr-channel", Name: "Canal LPR"}},
		nil,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	for who, actor := range map[string]authz.Actor{"owner": owner, "viewer": viewer} {
		got, err := svc.GetView(ctx, actor, created.ID)
		if err != nil {
			t.Fatalf("%s: %v", who, err)
		}
		if c := got.Layout.Cells[0]; c == nil || c.Vision == nil || c.Vision.ID != "visionai:lpr-channel" || c.Vision.Name != "Canal LPR" {
			t.Fatalf("%s: vision tile = %+v, want the LPR channel", who, c)
		}
		if got.Layout.Cells[1] != nil {
			t.Fatalf("%s: empty tile = %+v", who, got.Layout.Cells[1])
		}
	}
}
