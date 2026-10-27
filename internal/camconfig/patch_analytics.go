package camconfig

import (
	"maps"
	"slices"
)

// ModulePatch changes one analytics module: nil fields mean "leave as is".
type ModulePatch struct {
	Enabled  *bool                `json:"enabled,omitempty"`
	Settings *ModuleSettingsPatch `json:"settings,omitempty"`
}

// ModuleSettingsPatch changes the settings that have a real backing in the engine.
type ModuleSettingsPatch struct {
	MinArea     *int `json:"min_area,omitempty"`
	Enhancement *int `json:"enhancement,omitempty"`
}

const maxMinArea = 10_000_000

// Setting keys, in the order they are reviewed and diffed.
const (
	settingMinArea     = "min_area"
	settingEnhancement = "enhancement"
)

var settingKeys = []string{settingMinArea, settingEnhancement}

// settingRules says which modules take which setting and its range. Only settings Frigate has
// per camera are listed: thresholds, model and lists are server-wide there.
var settingRules = map[string]struct {
	modules  []string
	min, max int
	label    string
}{
	settingMinArea:     {[]string{ModuleLPR, ModuleFaceRecognition}, 0, maxMinArea, "área mínima"},
	settingEnhancement: {[]string{ModuleLPR}, 0, 10, "mejora de imagen"},
}

// value returns the patch value of a setting key, nil when unset. It is nil-safe.
func (s *ModuleSettingsPatch) value(key string) *int {
	if s == nil {
		return nil
	}
	switch key {
	case settingMinArea:
		return s.MinArea
	case settingEnhancement:
		return s.Enhancement
	}
	return nil
}

// value returns the effective value of a setting key, nil when the engine did not report it.
func (s ModuleSettings) value(key string) *int {
	switch key {
	case settingMinArea:
		return s.MinArea
	case settingEnhancement:
		return s.Enhancement
	}
	return nil
}

func (s *ModuleSettingsPatch) set(key string, v *int) {
	switch key {
	case settingMinArea:
		s.MinArea = v
	case settingEnhancement:
		s.Enhancement = v
	}
}

func (s *ModuleSettingsPatch) empty() bool {
	for _, k := range settingKeys {
		if s.value(k) != nil {
			return false
		}
	}
	return true
}

// copyPatch returns a copy of s without empty keys, nil when nothing is set.
func (s *ModuleSettingsPatch) copyPatch() *ModuleSettingsPatch {
	if s.empty() {
		return nil
	}
	out := &ModuleSettingsPatch{}
	for _, k := range settingKeys {
		if v := s.value(k); v != nil {
			c := *v
			out.set(k, &c)
		}
	}
	return out
}

// moduleSections maps the modules a Frigate camera can change to the camera section holding
// them. Every other module has no Frigate switch.
var moduleSections = map[string]string{
	ModuleLPR:             "lpr",
	ModuleFaceRecognition: "face_recognition",
}

func (m *ModulePatch) empty() bool {
	return m == nil || (m.Enabled == nil && (m.Settings == nil || m.Settings.empty()))
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
		for _, key := range settingKeys {
			v := m.Settings.value(key)
			if v == nil {
				continue
			}
			rule := settingRules[key]
			if !slices.Contains(rule.modules, id) {
				return invalid("analytics.modules.%s.settings.%s: this module has no such setting", id, key)
			}
			if *v < rule.min || *v > rule.max {
				return invalid("analytics.modules.%s.settings.%s: must be between %d and %d", id, key, rule.min, rule.max)
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
		n := &ModulePatch{Enabled: m.Enabled, Settings: m.Settings.copyPatch()}
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
			c.Settings = m.Settings.copyPatch()
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
		for _, key := range settingKeys {
			if v := m.Settings.value(key); v != nil {
				if cur.Settings = cur.Settings.copyPatch(); cur.Settings == nil {
					cur.Settings = &ModuleSettingsPatch{}
				}
				c := *v
				cur.Settings.set(key, &c)
			}
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
		for _, key := range settingKeys {
			want := mp.Settings.value(key)
			have := cur.Settings.value(key)
			if want == nil || (have != nil && *have == *want) {
				continue
			}
			var from any
			if have != nil {
				from = *have
			}
			changes = append(changes, sectionChange{section, Change{
				Path:  "analytics.modules." + id + ".settings." + key,
				Label: settingRules[key].label + " (" + label + ")", From: from, To: *want,
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
		if !ok && localModules[id] {
			continue // switched in OpenVMS, not in Frigate (see LocalChanges)
		}
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
		for _, key := range settingKeys {
			if v := mp.Settings.value(key); v != nil {
				cur[key] = *v
			}
		}
		out[section] = cur
	}
	return nil
}

// LocalChanges lists the OpenVMS-side module switches of a patch (module id -> enabled), which
// are stored by OpenVMS instead of being sent to Frigate.
func LocalChanges(patch Patch) map[string]bool {
	out := map[string]bool{}
	for id, m := range modulesOf(patch.Normalized()) {
		if localModules[id] && m.Enabled != nil {
			out[id] = *m.Enabled
		}
	}
	return out
}
