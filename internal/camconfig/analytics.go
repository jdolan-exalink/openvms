package camconfig

import (
	"slices"
	"strconv"
	"strings"
)

// Analytics module ids. Analytics are handled as modules: each one is shown as a card that can
// be switched on, and its detail only exists while it is on.
const (
	ModuleLPR                   = "lpr"
	ModuleFaceDetection         = "face_detection"
	ModuleFaceRecognition       = "face_recognition"
	ModuleVehicleClassification = "vehicle_classification"
	ModuleVehicleColor          = "vehicle_color"
	ModuleClothingColor         = "clothing_color"
	ModuleSimilarSearch         = "similar_search"
)

// Engines that can back a module (AnalyticsModule.Engine).
const (
	ModuleEngineFrigate   = "frigate"
	ModuleEngineProvision = "provision"
	ModuleEngineNative    = "native"
	ModuleEngineNone      = "none"
)

// Reasons shown when a module cannot be switched on.
const (
	ReasonNotAvailable    = "No disponible con este motor"
	ReasonConfiguredOnCam = "Se configura en la cámara"
)

// moduleOrder is the order of the cards and the label of each module.
var moduleOrder = []struct{ id, label string }{
	{ModuleLPR, "Matrículas (LPR)"},
	{ModuleFaceDetection, "Detección facial"},
	{ModuleFaceRecognition, "Reconocimiento facial"},
	{ModuleVehicleClassification, "Clasificación de vehículo"},
	{ModuleVehicleColor, "Color de vehículo"},
	{ModuleClothingColor, "Color de ropa"},
	{ModuleSimilarSearch, "Buscar similares cuando corresponda"},
}

// ModuleLabel returns the Spanish label of a module id and whether the id is known.
func ModuleLabel(id string) (string, bool) {
	for _, m := range moduleOrder {
		if m.id == id {
			return m.label, true
		}
	}
	return "", false
}

// AnalyticsModule is one analytics module of a camera, whatever engine runs it.
//
// A module is Available (and Enabled) only with positive evidence in the data OpenVMS already
// reads; otherwise Available is false and Reason says why. Nothing is invented.
type AnalyticsModule struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Available bool   `json:"available"`
	Enabled   bool   `json:"enabled"`
	// Writable says whether OpenVMS can switch the module through its engine. Provision cameras
	// report their LPR but cannot be configured remotely.
	Writable bool   `json:"writable"`
	Engine   string `json:"engine"`
	Reason   string `json:"reason,omitempty"`
	// Settings holds only fields that have a real backing in the engine.
	Settings ModuleSettings `json:"settings"`
	// Advanced is a read-only summary of the engine-specific parameters.
	Advanced []ModuleDetail `json:"advanced"`
}

// ModuleSettings are the module settings OpenVMS can read and write.
type ModuleSettings struct {
	// MinArea is the smallest object area (pixels) the engine analyses.
	MinArea *int `json:"min_area,omitempty"`
}

// ModuleDetail is one read-only line of a module's engine-specific summary.
type ModuleDetail struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Module returns the module with the given id.
func (a Analytics) Module(id string) (AnalyticsModule, bool) {
	for _, m := range a.Modules {
		if m.ID == id {
			return m, true
		}
	}
	return AnalyticsModule{}, false
}

// deriveModules builds Analytics.Modules from the evidence the Frigate adapter has: the
// inventory (tags, LPR flag) and the engine's camera document (nil when unreadable).
func deriveModules(cam *Camera, in Inventory, doc map[string]any) {
	lprTag := in.LPR || hasAnyTag(in.Tags, "lpr", "lpr_frigate", "lpr_provision")
	provision := hasAnyTag(in.Tags, "lpr_provision") || (lprTag && hasAnyTag(in.Tags, "provision", "provision_isr"))
	lprDoc, _ := doc["lpr"].(map[string]any)
	faceDoc, _ := doc["face_recognition"].(map[string]any)

	mods := make([]AnalyticsModule, 0, len(moduleOrder))
	for _, def := range moduleOrder {
		m := AnalyticsModule{
			ID: def.id, Label: def.label, Engine: ModuleEngineNone, Reason: ReasonNotAvailable,
			Advanced: []ModuleDetail{},
		}
		switch def.id {
		case ModuleLPR:
			switch {
			case provision:
				m.Available, m.Enabled, m.Engine, m.Reason = true, true, ModuleEngineProvision, ReasonConfiguredOnCam
				m.Advanced = append(m.Advanced, ModuleDetail{Label: "Origen", Value: "Analítica integrada Provision"})
			case lprDoc != nil || lprTag:
				m.Available, m.Engine, m.Reason = true, ModuleEngineFrigate, ""
				m.Enabled = isTrue(cam.Analytics.LPR)
				m.Writable = doc != nil
				frigateSettings(&m, lprDoc, "expire_time", "enhancement")
			}
		case ModuleFaceRecognition:
			if faceDoc != nil {
				m.Available, m.Engine, m.Reason = true, ModuleEngineFrigate, ""
				m.Enabled = isTrue(boolPtr(doc, "face_recognition", "enabled"))
				m.Writable = true
				frigateSettings(&m, faceDoc)
			}
		}
		mods = append(mods, m)
	}
	cam.Analytics.Modules = mods
}

// frigateSettings reads the settings and the read-only summary of a Frigate-backed module.
func frigateSettings(m *AnalyticsModule, section map[string]any, advanced ...string) {
	m.Advanced = append(m.Advanced, ModuleDetail{Label: "Motor", Value: "Frigate"})
	if f, ok := section["min_area"].(float64); ok {
		v := int(f)
		m.Settings.MinArea = &v
	}
	for _, k := range advanced {
		if v, ok := section[k]; ok && v != nil {
			m.Advanced = append(m.Advanced, ModuleDetail{Label: k, Value: detailText(v)})
		}
	}
}

func detailText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return "—"
}

// diffModules lists the module fields set in the desired patch whose effective value differs.
func diffModules(want *AnalyticsPatch, eff Analytics) []Drift {
	var out []Drift
	if want == nil {
		return out
	}
	for _, id := range sortedModuleIDs(want.Modules) {
		mp := want.Modules[id]
		cur, ok := eff.Module(id)
		ok = ok && cur.Available
		if mp.Enabled != nil {
			var got any
			if ok {
				got = cur.Enabled
			}
			if got != *mp.Enabled {
				out = append(out, Drift{Path: "analytics.modules." + id + ".enabled", Desired: *mp.Enabled, Effective: got})
			}
		}
		if mp.Settings != nil && mp.Settings.MinArea != nil {
			var got any
			if ok && cur.Settings.MinArea != nil {
				got = *cur.Settings.MinArea
			}
			if got != *mp.Settings.MinArea {
				out = append(out, Drift{Path: "analytics.modules." + id + ".settings.min_area", Desired: *mp.Settings.MinArea, Effective: got})
			}
		}
	}
	return out
}

func sortedModuleIDs(m map[string]*ModulePatch) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// CheckModules refuses a patch that touches a module the camera cannot change: not available
// with this engine, or reported but not configurable from OpenVMS. Nothing is dropped silently.
func CheckModules(current Camera, p Patch) error {
	var msgs []string
	for _, id := range sortedModuleIDs(modulesOf(p)) {
		if reason := moduleBlocked(current, id); reason != "" {
			label, _ := ModuleLabel(id)
			msgs = append(msgs, label+": "+reason)
		}
	}
	if len(msgs) > 0 {
		return invalid("analytics.modules: %s", strings.Join(msgs, "; "))
	}
	return nil
}

func modulesOf(p Patch) map[string]*ModulePatch {
	if p.Analytics == nil {
		return nil
	}
	return p.Analytics.Modules
}

// moduleBlocked returns why a module cannot be written, or "" when it can.
func moduleBlocked(current Camera, id string) string {
	m, ok := current.Analytics.Module(id)
	switch {
	case !ok || !m.Available:
		return ReasonNotAvailable
	case !m.Writable && m.Reason != "":
		return m.Reason
	case !m.Writable:
		return ReasonNotAvailable
	}
	return ""
}
