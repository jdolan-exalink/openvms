//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func TestAServerAddressCanBeRegisteredOnlyOncePerTenant(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	site := env.Cameras["frigate-h01/plaza"].SiteID
	taken := env.Mocks["frigate-h01"].URL // the demo server frigate-h01 already uses it
	create := func(name, url string) error {
		_, err := env.Svc.CreateServer(ctx, env.Admin, inventory.CreateServerInput{
			SiteID: site, Name: name, Conn: inventory.ConnInput{BaseURL: url, Username: "admin", Password: "h-pass"},
		})
		return err
	}

	for name, url := range map[string]string{
		"the same address":    taken,
		"a trailing slash":    taken + "/",
		"another letter case": strings.ToUpper(taken[:4]) + taken[4:],
	} {
		t.Run("creating with "+name+" is refused", func(t *testing.T) {
			err := create("dup-"+strings.ReplaceAll(name, " ", "-"), url)
			if !errors.Is(err, inventory.ErrDuplicateServer) {
				t.Fatalf("CreateServer = %v, want ErrDuplicateServer", err)
			}
		})
	}

	t.Run("editing a server onto an address another server uses is refused", func(t *testing.T) {
		other := env.Cameras["frigate-c01/muelle"].ServerID
		_, err := env.Svc.UpdateServer(ctx, env.Admin, other, inventory.UpdateServerInput{BaseURL: &taken})
		if !errors.Is(err, inventory.ErrDuplicateServer) {
			t.Fatalf("UpdateServer = %v, want ErrDuplicateServer", err)
		}
	})

	t.Run("editing a server without changing its address still works", func(t *testing.T) {
		id := env.Cameras["frigate-h01/plaza"].ServerID
		name := "frigate-h01-renamed"
		if _, err := env.Svc.UpdateServer(ctx, env.Admin, id, inventory.UpdateServerInput{Name: &name, BaseURL: &taken}); err != nil {
			t.Fatalf("UpdateServer = %v", err)
		}
	})

	t.Run("a deleted server frees its address", func(t *testing.T) {
		id := env.Cameras["frigate-h01/plaza"].ServerID
		if err := env.Svc.DeleteServer(ctx, env.Admin, id); err != nil {
			t.Fatal(err)
		}
		if err := create("frigate-again", taken); err != nil {
			t.Fatalf("CreateServer after delete = %v", err)
		}
	})
}
