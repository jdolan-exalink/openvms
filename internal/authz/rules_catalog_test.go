package authz

import "testing"

func TestRulePermissionsAreTenantScoped(t *testing.T) {
	for _, p := range []Permission{RulesRead, RulesManage} {
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
	if RulesRead != "rules.read" || RulesManage != "rules.manage" {
		t.Errorf("unexpected permission names: %q %q", RulesRead, RulesManage)
	}
}
