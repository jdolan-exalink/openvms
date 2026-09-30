package authz

import "testing"

func TestMapPermissionsAreSiteScoped(t *testing.T) {
	want := map[Permission]bool{
		MapsView:       false,
		MapsEdit:       false,
		MapsCreateZone: false,
		MapsEditDevice: false,
	}
	for _, d := range Catalog {
		if _, ok := want[d.Permission]; ok {
			want[d.Permission] = true
			if d.Narrowest != ScopeSite {
				t.Errorf("%s narrowest scope = %v, want site", d.Permission, d.Narrowest)
			}
			if d.Description == "" {
				t.Errorf("%s has no description", d.Permission)
			}
		}
	}
	for p, found := range want {
		if !found {
			t.Errorf("%s missing from the catalog", p)
		}
	}
	if MapsView != "maps.view" || MapsEdit != "maps.edit" || MapsCreateZone != "maps.create_zone" || MapsEditDevice != "maps.edit_device" {
		t.Errorf("unexpected permission names: %q %q %q %q", MapsView, MapsEdit, MapsCreateZone, MapsEditDevice)
	}
}
