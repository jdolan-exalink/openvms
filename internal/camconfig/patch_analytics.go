package camconfig

import (
	"maps"
)

// ModulePatch changes one analytics module: nil fields mean "leave as is".
type ModulePatch struct {
	Enabled  *bool                `json:"enabled,omitempty"`
	Settings *ModuleSettingsPatch `json:"settings,omitempty"`
}

// ModuleSettingsPatch changes the settings that have a real backing in the engine.
type ModuleSettingsPatch struct {
	MinArea *int `json:"min_area,omitempty"`
}

const maxMinArea = 10_000_000

// moduleSections maps the modules a Frigate camera can change to the camera section holding
// them. Every other module has no Frigate switch.
var moduleSections = map[string]string{
	ModuleLPR:             "lpr",
	ModuleFaceRecognition: "face_recognition",
}

func (m *ModulePatch) empty() bool {
	return m == nil || (m.Enabled == nil && (m.Settings == nil || m.Settings.MinArea == nil))
}

func analyticsEmpty(a *AnalyticsPatch) bool {
	if a == nil {
		return true
	}
	if a.LPR != nil {
		return false
	}
	for _, m := range a.Modules {
		if !m.empty() {
			return false
		}
	}
	return true
}

func (a *AnalyticsPatch) validate() error {
	if a == nil {
		return nil
	}
	for _, id := range sortedModuleIDs(a.Modules) {
		if _, ok := ModuleLabel(id); !ok {
			return invalid("analytics.modules: %q is not a known module", id)
		}
		m := a.Modules[id]
		if m.empty() {
			return invalid("analytics.modules.%s: nothing to change", id)
		}
		if m.Settings != nil && m.Settings.MinArea != nil {
			if _, ok := moduleSections[id]; !ok {
				return invalid("analytics.modules.%s.settings: this module has no settings to change", id)
			}
			if v := *m.Settings.MinArea; v < 0 || v > maxMinArea {
				return invalid("analytics.modules.%s.settings.min_area: must be between 0 and %d", id, maxMinArea)
			}
		}
		if id == ModuleLPR && m.Enabled != nil && a.LPR != nil && *m.Enabled != *a.LPR {
			return invalid("analytics: lpr and modules.lpr.enabled disagree")
		}
	}
	return nil
}

// normalized drops empty modules and the legacy lpr flag when modules.lpr.enabled says the
// same thing.
func (a *AnalyticsPatch) normalized() *AnalyticsPatch {
	if a == nil {
		return nil
	}
	out := &AnalyticsPatch{LPR: a.LPR}
	for id, m := range a.Modules {
		if m.empty() {
			continue
		}
		n := &ModulePatch{Enabled: m.Enabled}
		if m.Settings != nil && m.Settings.MinArea != nil {
			v := *m.Settings.MinArea
			n.Settings = &ModuleSettingsPatch{MinArea: &v}
		}
		if out.Modules == nil {
			out.Modules = map[string]*ModulePatch{}
		}
		out.Modules[id] = n
		if id == ModuleLPR && n.Enabled != nil {
			out.LPR = nil
		}
	}
	if analyticsEmpty(out) {
		return nil
	}
	return out
}

// mergeAnalytics overlays over onto base; the most recent word on the LPR switch wins whether
// it came as analytics.lpr or as modules.lpr.enabled.
func mergeAnalytics(base, over *AnalyticsPatch) *AnalyticsPatch {
	if over == nil {
		return base
	}
	out := &AnalyticsPatch{}
	if base != nil {
		out.LPR = base.LPR
		out.Modules = map[string]*ModulePatch{}
		for id, m := range base.Modules {
			c := *m
			out.Modules[id] = &c
		}
	} else {
		out.Modules = map[string]*ModulePatch{}
	}
	if over.LPR != nil {
		out.LPR = over.LPR
		if m := out.Modules[ModuleLPR]; m != nil {
			m.Enabled = nil
		}
	}
	for id, m := range over.Modules {
		cur := out.Modules[id]
		if cur == nil {
			cur = &ModulePatch{}
			out.Modules[id] = cur
		}
		if m.Enabled != nil {
			cur.Enabled = m.Enabled
			if id == ModuleLPR {
				out.LPR = nil
			}
		}
		if m.Settings != nil && m.Settings.MinArea != nil {
			cur.Settings = &ModuleSettingsPatch{MinArea: m.Settings.MinArea}
		}
	}
	return out.normalized()
}

// sectionChange is a review line together with the Frigate section it writes.
type sectionChange struct {
	section string
	change  Change
}

// planModules lists what the module changes would do and which of them the camera cannot take.
func planModules(current Camera, a *AnalyticsPatch) (changes []sectionChange, unsupported []string) {
	if a == nil {
		return nil, nil
	}
	for _, id := range sortedModuleIDs(a.Modules) {
		label, _ := ModuleLabel(id)
		if reason := moduleBlocked(current, id); reason != "" {
			unsupported = append(unsupported, "analytics.modules."+id+": "+label+": "+reason)
			continue
		}
		cur, _ := current.Analytics.Module(id)
		section := moduleSections[id]
		mp := a.Modules[id]
		if mp.Enabled != nil && cur.Enabled != *mp.Enabled {
			changes = append(changes, sectionChange{section, Change{
				Path: "analytics.modules." + id + ".enabled", Label: "módulo " + label, From: cur.Enabled, To: *mp.Enabled,
			}})
		}
		if mp.Settings != nil && mp.Settings.MinArea != nil && (cur.Settings.MinArea == nil || *cur.Settings.MinArea != *mp.Settings.MinArea) {
			var from any
			if cur.Settings.MinArea != nil {
				from = *cur.Settings.MinArea
			}
			changes = append(changes, sectionChange{section, Change{
				Path: "analytics.modules." + id + ".settings.min_area", Label: "área mínima (" + label + ")",
				From: from, To: *mp.Settings.MinArea,
			}})
		}
	}
	return changes, unsupported
}

// addModuleSections adds the Frigate sections of the module changes to out. A module Frigate
// has no switch for is an error, never dropped.
func addModuleSections(out map[string]any, a *AnalyticsPatch) error {
	if a == nil {
		return nil
	}
	for _, id := range sortedModuleIDs(a.Modules) {
		section, ok := moduleSections[id]
		if !ok {
			label, _ := ModuleLabel(id)
			return invalid("analytics.modules.%s: %s: %s", id, label, ReasonNotAvailable)
		}
		cur, _ := out[section].(map[string]any)
		if cur == nil {
			cur = map[string]any{}
		}
		cur = maps.Clone(cur)
		mp := a.Modules[id]
		if mp.Enabled != nil {
			cur["enabled"] = *mp.Enabled
		}
		if mp.Settings != nil && mp.Settings.MinArea != nil {
			cur["min_area"] = *mp.Settings.MinArea
		}
		out[section] = cur
	}
	return nil
}
