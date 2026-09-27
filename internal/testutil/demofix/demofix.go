//go:build integration

// Package demofix builds the demo tenant (bootstrap.Demo) on a throwaway database,
// against two in-process Frigate mocks, for integration tests.
package demofix

import (
	"context"
	"crypto/rand"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/frigatemock"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

type Env struct {
	Pool       *pgxpool.Pool
	Store      *store.Store
	Svc        *inventory.Service
	Admin      authz.Actor
	AdminToken string
	Demo       bootstrap.DemoResult
	// Cameras maps "<server>/<camera>" to the demo cameras.
	Cameras map[string]db.GetCameraRow
	// Mocks maps server name to its simulated Frigate (seeded with 30 reviews).
	Mocks map[string]*Mock
	Sealer *secrets.Sealer
}

// Mock is one simulated Frigate of the demo.
type Mock struct {
	URL       string
	Server    *frigatemock.Server
	Generator *frigatemock.Generator
}

func mock(t *testing.T, cameras, password string) *Mock {
	t.Helper()
	cams, err := frigatemock.ParseCameras(cameras)
	if err != nil {
		t.Fatal(err)
	}
	st := frigatemock.NewStore(1000)
	g := &frigatemock.Generator{Cameras: cams, Store: st}
	g.Seed(30, time.Hour, time.Now())
	srv := &frigatemock.Server{Version: "0.17.2-mock", Cameras: cams, Store: st, User: "admin", Password: password, RequireAuth: true, StartedAt: time.Now()}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &Mock{URL: ts.URL, Server: srv, Generator: g}
}

// Setup seeds the demo tenant: frigate-h01 (Helvecia: acceso_norte, plaza, cementerio,
// tesoreria) and frigate-c01 (Cayastá: ruta_1, plaza, muelle); Operator-A holds
// frigate-h01/acceso_norte and frigate-c01/muelle.
func Setup(t *testing.T) *Env {
	t.Helper()
	ctx := context.Background()
	pool, _ := pgtest.Migrated(t)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	sealer, err := secrets.NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	st := &store.Store{Pool: pool}
	svc := inventory.New(st, sealer, pgtest.Discard())

	admin, adminToken, err := bootstrap.PlatformAdmin(ctx, st, "admin")
	if err != nil {
		t.Fatal(err)
	}
	h := mock(t, "acceso_norte+lpr:entrada|salida,plaza:centro,cementerio:porton,tesoreria", "h-pass")
	c := mock(t, "ruta_1+lpr:ingreso,plaza:centro,muelle", "c-pass")
	res, err := bootstrap.Demo(ctx, svc, admin, []bootstrap.DemoServer{
		{Site: "Helvecia", Name: "frigate-h01", BaseURL: h.URL, Username: "admin", Password: "h-pass"},
		{Site: "Cayastá", Name: "frigate-c01", BaseURL: c.URL, Username: "admin", Password: "c-pass"},
	}, []string{"frigate-h01/acceso_norte", "frigate-c01/muelle"})
	if err != nil {
		t.Fatal(err)
	}

	env := &Env{Pool: pool, Store: st, Svc: svc, Admin: admin, AdminToken: adminToken, Demo: res, Cameras: map[string]db.GetCameraRow{},
		Mocks: map[string]*Mock{"frigate-h01": h, "frigate-c01": c}, Sealer: sealer}
	servers, err := svc.ListServers(ctx, admin, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, s := range servers {
		names[s.ID.String()] = s.Name
	}
	cams, err := svc.ListCameras(ctx, admin, inventory.CameraFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, cam := range cams {
		env.Cameras[names[cam.ServerID.String()]+"/"+cam.RemoteName] = cam
	}
	if len(env.Cameras) != 7 {
		t.Fatalf("demo has %d cameras, want 7", len(env.Cameras))
	}
	return env
}

// Actor resolves a demo user by name.
func (e *Env) Actor(t *testing.T, username string) authz.Actor {
	t.Helper()
	var a authz.Actor
	err := e.Store.Tx(context.Background(), store.AllTenants, func(q *db.Queries) error {
		u, err := q.GetUserByUsername(context.Background(), username)
		a = authz.Actor{UserID: u.ID, Username: u.Username, TenantID: u.TenantID}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
