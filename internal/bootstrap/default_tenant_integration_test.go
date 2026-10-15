//go:build integration

package bootstrap_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

type tenantsAndSites struct {
	tenants []db.Tenant
	sites   int
	names   []string
}

func readTenants(t *testing.T, st *store.Store) tenantsAndSites {
	t.Helper()
	ctx := context.Background()
	var out tenantsAndSites
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		out.tenants, err = q.ListTenants(ctx, nil)
		if err != nil {
			return err
		}
		for _, tn := range out.tenants {
			sites, err := q.ListSitesGeo(ctx, tn.ID)
			if err != nil {
				return err
			}
			for _, s := range sites {
				out.sites++
				out.names = append(out.names, s.Name)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEnsureDefaultTenantCreatesTenantAndSite(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, buf := captureLog()
	if err := bootstrap.EnsureDefaultTenant(ctx, st, "Mi Municipio", "Sitio principal", log); err != nil {
		t.Fatal(err)
	}
	got := readTenants(t, st)
	if len(got.tenants) != 1 || got.tenants[0].Name != "Mi Municipio" || got.tenants[0].Slug != "mi-municipio" {
		t.Fatalf("tenants = %+v", got.tenants)
	}
	if got.sites != 1 || got.names[0] != "Sitio principal" {
		t.Fatalf("sites = %v", got.names)
	}
	if !strings.Contains(buf.String(), "default tenant created") {
		t.Fatalf("log = %q", buf.String())
	}
	if err := bootstrap.EnsureDefaultTenant(ctx, st, "Mi Municipio", "Sitio principal", log); err != nil {
		t.Fatal(err)
	}
	if again := readTenants(t, st); len(again.tenants) != 1 || again.sites != 1 {
		t.Fatalf("second call changed data: %+v", again)
	}
}

func TestEnsureDefaultTenantLeavesExistingTenantsAlone(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, _ := captureLog()
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		_, err := q.CreateTenant(ctx, db.CreateTenantParams{Slug: "otro", Name: "Otro"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.EnsureDefaultTenant(ctx, st, "openvms", "Sitio principal", log); err != nil {
		t.Fatal(err)
	}
	if got := readTenants(t, st); len(got.tenants) != 1 || got.tenants[0].Slug != "otro" || got.sites != 0 {
		t.Fatalf("existing tenant touched: %+v", got)
	}
}

func TestEnsureDefaultTenantKeepsRename(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, _ := captureLog()
	if err := bootstrap.EnsureDefaultTenant(ctx, st, "openvms", "Sitio principal", log); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		ts, err := q.ListTenants(ctx, nil)
		if err != nil || len(ts) != 1 {
			return err
		}
		_, err = st.Pool.Exec(ctx, "UPDATE tenants SET name = 'Renombrado' WHERE id = $1", ts[0].ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.EnsureDefaultTenant(ctx, st, "openvms", "Sitio principal", log); err != nil {
		t.Fatal(err)
	}
	if got := readTenants(t, st); len(got.tenants) != 1 || got.tenants[0].Name != "Renombrado" {
		t.Fatalf("rename was reverted: %+v", got.tenants)
	}
}

func TestEnsureDefaultTenantConcurrent(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	log, _ := captureLog()
	errs := make(chan error, 4)
	for i := 0; i < cap(errs); i++ {
		go func() { errs <- bootstrap.EnsureDefaultTenant(ctx, st, "openvms", "Sitio principal", log) }()
	}
	for i := 0; i < cap(errs); i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got := readTenants(t, st); len(got.tenants) != 1 || got.sites != 1 {
		t.Fatalf("got %d tenants, %d sites", len(got.tenants), got.sites)
	}
}

func TestEnsureDefaultTenantSkipsEmptyNames(t *testing.T) {
	ctx := context.Background()
	st := adminFixture(t)
	for _, c := range [][2]string{{"  ", "Sitio"}, {"openvms", ""}, {"!!!", "Sitio"}} {
		log, buf := captureLog()
		if err := bootstrap.EnsureDefaultTenant(ctx, st, c[0], c[1], log); err != nil {
			t.Fatalf("%q: %v", c, err)
		}
		if !strings.Contains(buf.String(), "level=ERROR") {
			t.Fatalf("%q: want ERROR, got %q", c, buf.String())
		}
	}
	if got := readTenants(t, st); len(got.tenants) != 0 {
		t.Fatalf("tenants = %+v", got.tenants)
	}
}
