//go:build integration

package media_test

import (
	"context"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// A shared view carries its owner's display name so other users can tell whose it is.
func TestViewOwnerName(t *testing.T) {
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
	var display string
	if err := env.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		u, err := q.GetUserByUsername(ctx, "operador")
		display = u.DisplayName
		return err
	}); err != nil {
		t.Fatal(err)
	}
	svc := &media.Service{Store: env.Store, Adapters: inventory.NewAdapters(env.Svc), Log: pgtest.Discard()}
	created, err := svc.CreateView(ctx, owner, media.ViewInput{Name: "Turno", Shared: true, Layout: media.Layout{Columns: 1, Cells: []*media.Cell{}}})
	if err != nil {
		t.Fatal(err)
	}
	if created.OwnerName == nil || *created.OwnerName != display {
		t.Fatalf("created view owner_name = %v, want %q", created.OwnerName, display)
	}
	views, err := svc.ListViews(ctx, viewer)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].OwnerName == nil || *views[0].OwnerName != display {
		t.Fatalf("listed views = %+v, want one with owner_name %q", views, display)
	}
	got, err := svc.GetView(ctx, viewer, created.ID)
	if err != nil || got.OwnerName == nil || *got.OwnerName != display {
		t.Fatalf("GetView owner_name = %v (%v), want %q", got.OwnerName, err, display)
	}
}
