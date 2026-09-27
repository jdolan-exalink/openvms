package authz

import (
	"testing"

	"github.com/google/uuid"
)

var (
	tenant    = uuid.New()
	helvecia  = uuid.New()
	cayasta   = uuid.New()
	h01       = uuid.New()
	c01       = uuid.New()
	plaza     = uuid.New()
	tesoreria = uuid.New()
	rutaC     = uuid.New()
	accesos   = uuid.New()
)

func camera(id, site, server uuid.UUID, groups ...uuid.UUID) Resource {
	return Resource{Kind: ScopeCamera, ID: id, TenantID: tenant, SiteID: site, ServerID: server, GroupIDs: groups}
}

func TestSiteGrantWithCameraDeny(t *testing.T) {
	// PRD §30: ALLOW live.view SITE Helvecia + DENY live.view CAMERA Tesoreria.
	grants := []Grant{
		{LiveView, Allow, Scope{ScopeSite, helvecia}},
		{LiveView, Deny, Scope{ScopeCamera, tesoreria}},
	}
	cases := []struct {
		name string
		res  Resource
		want bool
	}{
		{"camera in site", camera(plaza, helvecia, h01), true},
		{"denied camera in site", camera(tesoreria, helvecia, h01), false},
		{"camera in another site", camera(rutaC, cayasta, c01), false},
	}
	for _, tc := range cases {
		if got := Decide(grants, LiveView, tc.res.Covering()); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPermissionsDoNotLeakAcrossNames(t *testing.T) {
	grants := []Grant{{LiveView, Allow, Scope{ScopeSite, helvecia}}}
	if Decide(grants, RecordingsView, camera(plaza, helvecia, h01).Covering()) {
		t.Error("live.view must not imply recordings.view")
	}
}

func TestNoGrantsMeansDenied(t *testing.T) {
	if Decide(nil, LiveView, camera(plaza, helvecia, h01).Covering()) {
		t.Error("default must be deny")
	}
}

func TestCameraGroupCrossesSites(t *testing.T) {
	// PRD §33: Policía ALLOW live.view SCOPE camera_group:Accesos, spanning sites.
	grants := []Grant{{LiveView, Allow, Scope{ScopeCameraGroup, accesos}}}
	if !Decide(grants, LiveView, camera(plaza, helvecia, h01, accesos).Covering()) {
		t.Error("member of Accesos in Helvecia should be allowed")
	}
	if !Decide(grants, LiveView, camera(rutaC, cayasta, c01, accesos).Covering()) {
		t.Error("member of Accesos in Cayastá should be allowed")
	}
	if Decide(grants, LiveView, camera(tesoreria, helvecia, h01).Covering()) {
		t.Error("non-member must be denied")
	}
}

func TestDenyAtBroaderScopeBeatsNarrowAllow(t *testing.T) {
	grants := []Grant{
		{LiveView, Deny, Scope{ScopeSite, helvecia}},
		{LiveView, Allow, Scope{ScopeCamera, plaza}},
	}
	if Decide(grants, LiveView, camera(plaza, helvecia, h01).Covering()) {
		t.Error("DENY > ALLOW regardless of scope depth")
	}
}

func TestServerGrantCoversItsCamerasOnly(t *testing.T) {
	grants := []Grant{{LiveView, Allow, Scope{ScopeServer, h01}}}
	if !Decide(grants, LiveView, camera(plaza, helvecia, h01).Covering()) {
		t.Error("camera on H01 should be allowed")
	}
	other := uuid.New()
	if Decide(grants, LiveView, camera(uuid.New(), helvecia, other).Covering()) {
		t.Error("camera on another server of the same site must be denied")
	}
}

func TestPlatformAndTenantGrants(t *testing.T) {
	platform := []Grant{{ServersManage, Allow, Scope{Type: ScopePlatform}}}
	server := Resource{Kind: ScopeServer, ID: h01, TenantID: tenant, SiteID: helvecia}
	if !Decide(platform, ServersManage, server.Covering()) {
		t.Error("platform grant covers every server")
	}
	otherTenant := []Grant{{ServersManage, Allow, Scope{ScopeTenant, uuid.New()}}}
	if Decide(otherTenant, ServersManage, server.Covering()) {
		t.Error("a grant on another tenant must not apply")
	}
}

func TestGrantable(t *testing.T) {
	cases := []struct {
		p    Permission
		s    ScopeType
		want bool
	}{
		{LiveView, ScopeCamera, true},
		{LiveView, ScopeCameraGroup, true},
		{UsersManage, ScopeCamera, false},
		{UsersManage, ScopeCameraGroup, false},
		{UsersManage, ScopeTenant, true},
		{ServersManage, ScopeServer, true},
		{ServersManage, ScopeCamera, false},
		{SitesManage, ScopeServer, false},
		{"made.up", ScopeTenant, false},
	}
	for _, tc := range cases {
		if got := Grantable(tc.p, tc.s); got != tc.want {
			t.Errorf("Grantable(%s, %s) = %v, want %v", tc.p, tc.s, got, tc.want)
		}
	}
}
