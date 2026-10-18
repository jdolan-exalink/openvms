package authz

import "testing"

func TestFacePermissionsAreTenantScoped(t *testing.T) {
	for _, p := range []Permission{FacesView, FacesManage} {
		d, ok := Lookup(p)
		if !ok {
			t.Fatalf("%s missing from the catalog", p)
		}
		if d.Narrowest != ScopeTenant {
			t.Errorf("%s narrowest scope = %v, want tenant", p, d.Narrowest)
		}
		if d.Description == "" {
			t.Errorf("%s has no description", p)
		}
	}
	if FacesView != "faces.view" || FacesManage != "faces.manage" {
		t.Errorf("unexpected permission names: %q %q", FacesView, FacesManage)
	}
}
