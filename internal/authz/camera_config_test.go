package authz

import (
	"testing"

	"github.com/google/uuid"
)

func TestCameraConfigCatalog(t *testing.T) {
	for _, p := range CameraConfigFamily {
		d, ok := Lookup(p)
		if !ok || d.Description == "" || d.Narrowest != ScopeCamera {
			t.Errorf("%s: missing or malformed catalog entry %+v", p, d)
		}
		if _, ok := CompatFor(p); !ok {
			t.Errorf("%s: no compat rule", p)
		}
		if !Grantable(p, ScopeServer) || !Grantable(p, ScopeTenant) {
			t.Errorf("%s must be grantable at server and tenant scope", p)
		}
	}
}

// legacyAllowed mirrors access.Checker.CanCompat so the matrix runs without a database.
func legacyAllowed(grants []Grant, p Permission, cov []Scope) bool {
	c, ok := CompatFor(p)
	if !ok {
		return false
	}
	for _, l := range c.Legacy {
		if !Decide(grants, l, cov) {
			return false
		}
	}
	return true
}

func TestDecideCompatMatrix(t *testing.T) {
	tenant, server := uuid.New(), uuid.New()
	res := Resource{Kind: ScopeCamera, ID: uuid.New(), TenantID: tenant, SiteID: uuid.New(), ServerID: server}
	cov := res.Covering()
	ts := Scope{ScopeTenant, tenant}
	ss := Scope{ScopeServer, server}
	allow := func(p Permission, s Scope) Grant { return Grant{p, Allow, s} }
	deny := func(p Permission, s Scope) Grant { return Grant{p, Deny, s} }
	decide := func(g []Grant, p Permission) bool { return DecideCompat(g, p, cov, legacyAllowed(g, p, cov)) }

	t.Run("nobody gains access", func(t *testing.T) {
		for _, p := range CameraConfigFamily {
			if decide(nil, p) {
				t.Errorf("%s allowed without grants", p)
			}
		}
		onlyView := []Grant{allow(CamerasView, ts)}
		for _, p := range CameraConfigFamily {
			if p != CamerasConfigView && decide(onlyView, p) {
				t.Errorf("cameras.view alone must not grant %s", p)
			}
		}
		onlyManage := []Grant{allow(CamerasManage, ts)}
		for _, p := range CameraConfigFamily {
			if p != CamerasConfigPTZ && decide(onlyManage, p) {
				t.Errorf("cameras.manage alone must not grant %s", p)
			}
		}
	})
	t.Run("legacy holders keep access", func(t *testing.T) {
		cases := []struct {
			name   string
			grants []Grant
			want   []Permission
		}{
			{"cameras.view", []Grant{allow(CamerasView, ts)}, []Permission{CamerasConfigView}},
			{"cameras.manage", []Grant{allow(CamerasManage, ss)}, []Permission{CamerasConfigPTZ}},
			{"servers.config", []Grant{allow(ServersConfig, ss)}, []Permission{CamerasConfigEdit, CamerasConfigVideo, CamerasConfigRecording, CamerasConfigAnalytics, CamerasConfigAdvanced}},
			{"web pair", []Grant{allow(ServersManage, ts), allow(ServersConfigSecrets, ss)}, []Permission{CamerasWebAccess}},
			{"yaml pair", []Grant{allow(ServersConfig, ts), allow(ServersConfigSecrets, ts)}, []Permission{CamerasConfigYAML}},
			{"restart", []Grant{allow(ServersRestart, ss)}, []Permission{CamerasRestart}},
		}
		for _, c := range cases {
			for _, p := range c.want {
				if !decide(c.grants, p) {
					t.Errorf("%s lost %s", c.name, p)
				}
			}
		}
		// servers.config alone never reaches web access or YAML.
		g := []Grant{allow(ServersConfig, ts)}
		if decide(g, CamerasWebAccess) || decide(g, CamerasConfigYAML) {
			t.Error("servers.config alone must not open web access or YAML")
		}
	})
	t.Run("granular strict mode", func(t *testing.T) {
		// Migrated holder that later lost video: the legacy grant no longer covers it.
		g := []Grant{allow(ServersConfig, ss), allow(CamerasConfigEdit, ss)}
		if decide(g, CamerasConfigVideo) {
			t.Error("revoking a granular grant must take effect")
		}
		if !decide(g, CamerasConfigEdit) {
			t.Error("granted permission lost")
		}
		// The web pair is always accepted.
		w := []Grant{allow(ServersManage, ts), allow(ServersConfigSecrets, ts), allow(CamerasConfigEdit, ts)}
		if !decide(w, CamerasWebAccess) {
			t.Error("web pair must stay equivalent")
		}
	})
	t.Run("deny wins", func(t *testing.T) {
		g := []Grant{allow(ServersConfig, ts), deny(CamerasConfigVideo, ss)}
		if decide(g, CamerasConfigVideo) {
			t.Error("explicit deny ignored")
		}
		if !decide(g, CamerasConfigRecording) {
			t.Error("deny leaked to another section")
		}
		w := []Grant{allow(ServersManage, ts), allow(ServersConfigSecrets, ts), deny(CamerasWebAccess, ts)}
		if decide(w, CamerasWebAccess) {
			t.Error("deny must beat the legacy pair")
		}
		wrongTenant := []Grant{allow(CamerasConfigVideo, Scope{ScopeTenant, uuid.New()})}
		if decide(wrongTenant, CamerasConfigVideo) {
			t.Error("grant in another tenant must not apply")
		}
	})
}
