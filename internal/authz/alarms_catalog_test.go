package authz

import "testing"

func TestAlarmPermissionsAreCameraScoped(t *testing.T) {
	want := map[Permission]bool{AlarmsView: false, AlarmsManage: false}
	for _, d := range Catalog {
		if _, ok := want[d.Permission]; ok {
			want[d.Permission] = true
			if d.Narrowest != ScopeCamera {
				t.Errorf("%s narrowest scope = %v, want camera", d.Permission, d.Narrowest)
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
	if AlarmsView != "alarms.view" || AlarmsManage != "alarms.manage" {
		t.Errorf("unexpected permission names: %q %q", AlarmsView, AlarmsManage)
	}
}
