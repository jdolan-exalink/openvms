package authz

// CameraConfigFamily lists the granular camera permissions (PRD §40). An actor holding none of
// them (allow, any scope) is in legacy mode.
var CameraConfigFamily = []Permission{
	CamerasConfigView, CamerasConfigEdit, CamerasConfigVideo, CamerasConfigRecording,
	CamerasConfigAnalytics, CamerasConfigPTZ, CamerasWebAccess, CamerasConfigAdvanced,
	CamerasConfigYAML, CamerasRestart,
}

// Compat describes how a granular permission relates to the legacy permissions that allowed the
// same action before it existed.
type Compat struct {
	// Legacy is the set of legacy permissions that ALL must be allowed (at the same resource).
	Legacy []Permission
	// Always accepts the legacy set even when the actor holds other granular grants. Otherwise
	// the legacy set only counts in legacy mode, so removing a granular grant takes effect.
	Always bool
}

var compat = map[Permission]Compat{
	CamerasConfigView:      {Legacy: []Permission{CamerasView}, Always: true},
	CamerasConfigEdit:      {Legacy: []Permission{ServersConfig}},
	CamerasConfigVideo:     {Legacy: []Permission{ServersConfig}},
	CamerasConfigRecording: {Legacy: []Permission{ServersConfig}},
	CamerasConfigAnalytics: {Legacy: []Permission{ServersConfig}},
	CamerasConfigPTZ:       {Legacy: []Permission{CamerasManage}},
	CamerasConfigAdvanced:  {Legacy: []Permission{ServersConfig}},
	CamerasConfigYAML:      {Legacy: []Permission{ServersConfig, ServersConfigSecrets}},
	CamerasWebAccess:       {Legacy: []Permission{ServersManage, ServersConfigSecrets}, Always: true},
	CamerasRestart:         {Legacy: []Permission{ServersRestart}},
}

// CompatFor returns the compatibility rule of a granular permission.
func CompatFor(p Permission) (Compat, bool) {
	c, ok := compat[p]
	return c, ok
}

// DecideCompat decides a granular permission: an explicit DENY wins; an ALLOW grants; otherwise
// the legacy set (legacyAllowed, computed by the caller on the same resource) grants either
// always or only while the actor has no granular grant at all (legacy mode). It never grants
// more than the legacy permissions did.
func DecideCompat(grants []Grant, perm Permission, covering []Scope, legacyAllowed bool) bool {
	in := make(map[Scope]bool, len(covering))
	for _, s := range covering {
		in[s] = true
	}
	for _, g := range grants {
		if g.Permission == perm && g.Effect == Deny && in[g.Scope] {
			return false
		}
	}
	if Decide(grants, perm, covering) {
		return true
	}
	c, ok := compat[perm]
	if !ok || !legacyAllowed {
		return false
	}
	if c.Always {
		return true
	}
	for _, g := range grants {
		for _, f := range CameraConfigFamily {
			if g.Permission == f && g.Effect == Allow {
				return false
			}
		}
	}
	return true
}
