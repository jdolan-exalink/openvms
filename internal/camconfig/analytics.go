package camconfig

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/jdolan-exalink/openvms/internal/frigate"
)

// Analytics module ids. Analytics are handled as modules: each one is shown as a card that can
// be switched on, and its detail only exists while it is on.
const (
	ModuleLPR                   = "lpr"
	ModuleFaceDetection         = "face_detection"
	ModuleFaceRecognition       = "face_recognition"
	ModuleLineCrossing          = "line_crossing"
	ModuleIntrusion             = "intrusion"
	ModulePeopleCounting        = "people_counting"
	ModuleVehicleClassification = "vehicle_classification"
	ModuleVehicleColor          = "vehicle_color"
	ModuleClothingColor         = "clothing_color"
	ModuleSimilarSearch         = "similar_search"
)

// Engines that can back a module (AnalyticsModule.Engine).
const (
	ModuleEngineFrigate   = "frigate"
	ModuleEngineProvision = "provision"
	ModuleEngineONVIF     = "onvif"
	ModuleEngineNative    = "native"
	ModuleEngineOpenVMS   = "openvms"
	ModuleEngineNone      = "none"
)

// OriginLabel returns the human-readable origin required:
// "Internas de Frigate", "Internas de cámaras Provision", "ONVIF", "OpenVMS".
func OriginLabel(engine string) string {
	switch engine {
	case ModuleEngineFrigate:
		return "Internas de Frigate"
	case ModuleEngineProvision:
		return "Internas de cámaras Provision"
	case ModuleEngineONVIF:
		return "ONVIF"
	case ModuleEngineOpenVMS:
		return "OpenVMS"
	default:
		return ""
	}
}

// Reasons shown when a module cannot be switched on.
const (
	ReasonNotAvailable    = "No disponible con este motor"
	ReasonConfiguredOnCam = "Se configura en la cámara"

	// Specific reasons: each says what is missing instead of the generic text.
	ReasonFaceDetectionImplicit = "Frigate detecta rostros solo mientras el reconocimiento facial está activo; no tiene un interruptor de detección aparte"
	ReasonSimilarSearch         = "Frigate ofrece la búsqueda semántica para todo el servidor, no por cámara, y OpenVMS no calcula similitud de vehículos"
)

// localModules are the modules OpenVMS runs itself; the others are switched in Frigate.
var localModules = map[string]bool{ModuleVehicleClassification: true, ModuleVehicleColor: true, ModuleClothingColor: true}

// faceGlobals is the optional part of an engine that reports Frigate's server-wide
// face_recognition configuration (thresholds and model are not per camera in Frigate).
type faceGlobals interface {
	FaceRecognitionSettings(ctx context.Context) (frigate.FaceRecognitionSettings, error)
}

// moduleOrder is the order of the cards and the label of each module.
var moduleOrder = []struct{ id, label string }{
	{ModuleLPR, "LPR"},
	{ModuleFaceDetection, "Detección facial"},
	{ModuleFaceRecognition, "Reconocimiento facial"},
	{ModuleLineCrossing, "Cruce de línea"},
	{ModuleIntrusion, "Intrusión en área"},
	{ModulePeopleCounting, "Conteo de personas"},
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
	ID          string `json:"id"`
	Label       string `json:"label"`
	OriginLabel string `json:"origin_label,omitempty"`
	Available   bool   `json:"available"`
	Enabled     bool   `json:"enabled"`
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
	// Enhancement is Frigate's per-camera LPR image enhancement level (0 to 10).
	Enhancement *int `json:"enhancement,omitempty"`
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
// inventory (tags, LPR flag, OpenVMS-side switches) and the engine's camera document (nil when
// unreadable). face holds Frigate's server-wide face settings when they could be read.
func deriveModules(cam *Camera, in Inventory, doc map[string]any) {
	deriveModulesWith(cam, in, doc, nil)
}

// deriveModulesWith is deriveModules with Frigate's server-wide face settings, when known.
func deriveModulesWith(cam *Camera, in Inventory, doc map[string]any, face *frigate.FaceRecognitionSettings) {
	lprTag := in.LPR || hasAnyTag(in.Tags, "lpr", "lpr_frigate", "lpr_provision")
	provision := hasAnyTag(in.Tags, "lpr_provision") || (lprTag && hasAnyTag(in.Tags, "provision", "provision_isr"))
	lprDoc, _ := doc["lpr"].(map[string]any)
	faceDoc, _ := doc["face_recognition"].(map[string]any)
	caps, haveCaps := readONVIFCaps(in.ONVIFCapabilities)
	hasOnvifAnalytics := haveCaps && caps.HasAnalytics

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
				m.Label = "LPR Provision-ISR"
				m.OriginLabel = OriginLabel(ModuleEngineProvision)
				m.Advanced = append(m.Advanced, ModuleDetail{Label: "Origen", Value: "Analítica integrada Provision"})
			case lprDoc != nil || lprTag:
				m.Available, m.Engine, m.Reason = true, ModuleEngineFrigate, ""
				m.Label = "LPR"
				m.OriginLabel = OriginLabel(ModuleEngineFrigate)
				m.Enabled = isTrue(cam.Analytics.LPR)
				m.Writable = doc != nil
				frigateSettings(&m, lprDoc, "expire_time")
				m.Advanced = append(m.Advanced, ModuleDetail{Label: "Resultados", Value: "OpenVMS guarda cada lectura en el evento y permite buscar matrículas"})
			default:
				m.Label = "LPR"
			}
		case ModuleFaceRecognition:
			if faceDoc != nil {
				m.Available, m.Engine, m.Reason = true, ModuleEngineFrigate, ""
				m.OriginLabel = OriginLabel(ModuleEngineFrigate)
				m.Enabled = isTrue(boolPtr(doc, "face_recognition", "enabled"))
				m.Writable = true
				frigateSettings(&m, faceDoc)
				m.Advanced = append(m.Advanced, faceGlobalDetails(face)...)
				m.Advanced = append(m.Advanced, ModuleDetail{Label: "Base de rostros", Value: "Las listas de vigilancia son del tenant y se aplican en las reglas de alarma; no hay una lista por cámara"})
			}
		case ModuleFaceDetection:
			// Frigate runs the face detector only as the first step of recognition: there is no
			// "detect without recognizing" mode, so the module mirrors recognition and is read-only.
			if faceDoc != nil {
				m.Available, m.Engine, m.Reason = true, ModuleEngineFrigate, ReasonFaceDetectionImplicit
				m.OriginLabel = OriginLabel(ModuleEngineFrigate)
				m.Enabled = isTrue(boolPtr(doc, "face_recognition", "enabled"))
				m.Advanced = append(m.Advanced, ModuleDetail{Label: "Motor", Value: "Frigate"})
				if face != nil {
					m.Advanced = append(m.Advanced, ModuleDetail{Label: "Umbral de detección (servidor)", Value: strconv.FormatFloat(face.DetectionThreshold, 'f', -1, 64)})
				}
			}
		case ModuleLineCrossing, ModuleIntrusion, ModulePeopleCounting:
			switch {
			case provision:
				m.Available, m.Enabled, m.Engine, m.Reason = true, true, ModuleEngineProvision, ReasonConfiguredOnCam
				m.OriginLabel = OriginLabel(ModuleEngineProvision)
				m.Advanced = append(m.Advanced, ModuleDetail{Label: "Origen", Value: "Analítica inteligente integrada de la cámara Provision-ISR"})
			case hasOnvifAnalytics:
				m.Available, m.Enabled, m.Engine, m.Reason = true, true, ModuleEngineONVIF, ReasonConfiguredOnCam
				m.OriginLabel = OriginLabel(ModuleEngineONVIF)
				m.Advanced = append(m.Advanced, ModuleDetail{Label: "Origen", Value: "Reglas de analítica inteligente ONVIF (Profile M / T)"})
			default:
				m.Reason = ReasonNotAvailable
			}
		case ModuleVehicleClassification, ModuleVehicleColor, ModuleClothingColor:
			localModule(&m, in.Local)
			if m.Available {
				m.OriginLabel = OriginLabel(ModuleEngineOpenVMS)
			}
		case ModuleSimilarSearch:
			m.Reason = ReasonSimilarSearch
		}
		if m.OriginLabel == "" && m.Engine != ModuleEngineNone {
			m.OriginLabel = OriginLabel(m.Engine)
		}
		mods = append(mods, m)
	}
	cam.Analytics.Modules = mods
}

// localSummary says how each OpenVMS-side module works, for the read-only detail.
var localSummary = map[string][2]string{
	ModuleVehicleClassification: {"Tipo de vehículo (sedán, SUV, camioneta, furgón...)", "Clasificador de carrocería del worker de OpenVMS sobre la captura del evento; sin el modelo instalado se conserva la etiqueta de Frigate"},
	ModuleVehicleColor:          {"Color de vehículo", "Color calculado por OpenVMS desde la captura del evento de vehículo"},
	ModuleClothingColor:         {"Color de ropa", "Colores de prenda superior e inferior calculados por OpenVMS desde la captura del evento de persona"},
}

// localModule fills a module OpenVMS runs itself from the switches in the inventory.
func localModule(m *AnalyticsModule, local *LocalAnalytics) {
	if local == nil {
		return
	}
	on, known := local.Enabled[m.ID]
	if !known {
		return
	}
	sum := localSummary[m.ID]
	m.Available, m.Enabled, m.Writable, m.Engine, m.Reason = true, on, true, ModuleEngineOpenVMS, ""
	m.Advanced = append(m.Advanced,
		ModuleDetail{Label: "Motor", Value: "OpenVMS"},
		ModuleDetail{Label: "Cómo funciona", Value: sum[1]},
		ModuleDetail{Label: "Alcance", Value: "Eventos nuevos de esta cámara"})
	if why := local.Locked[m.ID]; why != "" {
		m.Writable, m.Reason = false, why
	}
}

// faceGlobalDetails lists Frigate's server-wide face settings: thresholds and model are not
// per camera, so they are read-only here.
func faceGlobalDetails(f *frigate.FaceRecognitionSettings) []ModuleDetail {
	if f == nil {
		return nil
	}
	num := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return []ModuleDetail{
		{Label: "Modelo (servidor)", Value: f.ModelSize},
		{Label: "Umbral de detección (servidor)", Value: num(f.DetectionThreshold)},
		{Label: "Umbral de reconocimiento (servidor)", Value: num(f.RecognitionThreshold)},
	}
}

// frigateSettings reads the settings and the read-only summary of a Frigate-backed module.
func frigateSettings(m *AnalyticsModule, section map[string]any, advanced ...string) {
	m.Advanced = append(m.Advanced, ModuleDetail{Label: "Motor", Value: "Frigate"})
	if f, ok := section["min_area"].(float64); ok {
		v := int(f)
		m.Settings.MinArea = &v
	}
	if m.ID == ModuleLPR {
		if f, ok := section["enhancement"].(float64); ok {
			v := int(f)
			m.Settings.Enhancement = &v
		}
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
		for _, key := range settingKeys {
			want := mp.Settings.value(key)
			if want == nil {
				continue
			}
			var got any
			if ok {
				if v := cur.Settings.value(key); v != nil {
					got = *v
				}
			}
			if got != *want {
				out = append(out, Drift{Path: "analytics.modules." + id + ".settings." + key, Desired: *want, Effective: got})
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
