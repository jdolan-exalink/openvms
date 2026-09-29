package bootstrap

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// DemoServer is a Frigate server to register in the demo tenant.
type DemoServer struct {
	Site     string
	Name     string
	BaseURL  string
	Username string
	Password string
}

// DemoResult lists what Demo created, including tokens shown once.
type DemoResult struct {
	TenantID uuid.UUID
	Tokens   map[string]string
}

// operatorPerms is what the Operator-A group holds on its cameras (PRD §129).
var operatorPerms = []authz.Permission{
	authz.CamerasView, authz.LiveView, authz.EventsView, authz.EventsSearch,
	authz.AlarmsView, authz.AlarmsManage,
	authz.RecordingsView, authz.RecordingsSeek, authz.SnapshotsView,
}

// Demo builds the "demo" tenant through the inventory service, acting as admin, so
// every step is authorized and audited like a real one:
//   - sites Helvecia, Cayastá and Santa Rosa; servers registered from servers.
//   - group Operator-A (user "operador") allowed on one camera of each server (PRD §129).
//   - user "supervisor" allowed on the whole Helvecia site except its first camera
//     named "tesoreria", which is denied (DENY > ALLOW).
func Demo(ctx context.Context, svc *inventory.Service, admin authz.Actor, servers []DemoServer, operatorCameras []string) (DemoResult, error) {
	res := DemoResult{Tokens: map[string]string{}}
	tenant, err := svc.CreateTenant(ctx, admin, "demo", "Demo")
	if err != nil {
		return res, fmt.Errorf("create tenant demo (already seeded?): %w", err)
	}
	res.TenantID = tenant.ID

	sites := map[string]uuid.UUID{}
	for _, name := range []string{"Helvecia", "Cayastá", "Santa Rosa"} {
		site, err := svc.CreateSite(ctx, admin, tenant.ID, inventory.SiteInput{Name: &name, Timezone: ptr("America/Argentina/Buenos_Aires")})
		if err != nil {
			return res, fmt.Errorf("create site %s: %w", name, err)
		}
		sites[name] = site.ID
	}
	serverIDs := map[string]uuid.UUID{}
	for _, s := range servers {
		siteID, ok := sites[s.Site]
		if !ok {
			return res, fmt.Errorf("server %s: unknown site %q", s.Name, s.Site)
		}
		v, err := svc.CreateServer(ctx, admin, inventory.CreateServerInput{
			SiteID: siteID, Name: s.Name, ImportCameras: true,
			Conn: inventory.ConnInput{BaseURL: s.BaseURL, Username: s.Username, Password: s.Password},
		})
		if err != nil {
			return res, fmt.Errorf("register server %s: %w", s.Name, err)
		}
		serverIDs[s.Name] = v.ID
	}

	cams, err := svc.ListCameras(ctx, admin, inventory.CameraFilter{})
	if err != nil {
		return res, err
	}
	// camera keys are "<server name>/<remote name>".
	camByKey := map[string]db.GetCameraRow{}
	serverName := map[uuid.UUID]string{}
	for n, id := range serverIDs {
		serverName[id] = n
	}
	for _, c := range cams {
		camByKey[serverName[c.ServerID]+"/"+c.RemoteName] = c
	}

	// Operator-A: user group with per-camera grants.
	var groupID uuid.UUID
	err = svc.Store.Tx(ctx, store.TenantScope{TenantID: tenant.ID}, func(q *db.Queries) error {
		g, err := q.CreateUserGroup(ctx, db.CreateUserGroupParams{TenantID: &tenant.ID, Name: "Operator-A", Description: "Operadores con acceso a cámaras puntuales"})
		groupID = g.ID
		return err
	})
	if err != nil {
		return res, fmt.Errorf("create group: %w", err)
	}
	operator, token, err := TenantUser(ctx, svc.Store, tenant.ID, "operador")
	if err != nil {
		return res, err
	}
	res.Tokens["operador"] = token
	err = svc.Store.Tx(ctx, store.TenantScope{TenantID: tenant.ID}, func(q *db.Queries) error {
		return q.AddUserToGroup(ctx, db.AddUserToGroupParams{GroupID: groupID, UserID: operator.UserID})
	})
	if err != nil {
		return res, err
	}
	for _, key := range operatorCameras {
		cam, ok := camByKey[key]
		if !ok {
			return res, fmt.Errorf("operator camera %q not found", key)
		}
		for _, p := range operatorPerms {
			if _, err := svc.CreateGrant(ctx, admin, inventory.GrantInput{
				SubjectType: "group", SubjectID: groupID, Permission: p, Effect: authz.Allow,
				ScopeType: authz.ScopeCamera, ScopeID: &cam.ID,
			}); err != nil {
				return res, fmt.Errorf("grant %s on %s: %w", p, key, err)
			}
		}
	}

	// Supervisor: the whole Helvecia site, minus the treasury camera.
	sup, token, err := TenantUser(ctx, svc.Store, tenant.ID, "supervisor")
	if err != nil {
		return res, err
	}
	res.Tokens["supervisor"] = token
	helvecia := sites["Helvecia"]
	for _, p := range append([]authz.Permission{authz.SitesView, authz.ServersView, authz.HealthView}, operatorPerms...) {
		if _, err := svc.CreateGrant(ctx, admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: sup.UserID, Permission: p, Effect: authz.Allow,
			ScopeType: authz.ScopeSite, ScopeID: &helvecia,
		}); err != nil {
			return res, fmt.Errorf("grant %s to supervisor: %w", p, err)
		}
	}
	for _, c := range cams {
		if c.SiteID == helvecia && c.RemoteName == "tesoreria" {
			for _, p := range operatorPerms {
				if _, err := svc.CreateGrant(ctx, admin, inventory.GrantInput{
					SubjectType: "user", SubjectID: sup.UserID, Permission: p, Effect: authz.Deny,
					ScopeType: authz.ScopeCamera, ScopeID: &c.ID,
				}); err != nil {
					return res, fmt.Errorf("deny %s on tesoreria: %w", p, err)
				}
			}
			break
		}
	}
	return res, nil
}

func ptr[T any](v T) *T { return &v }
