package visionai

import (
	"fmt"
	"slices"

	"github.com/google/uuid"
)

// ValidationError reports an invalid caller input (unknown source or layout).
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// Sources lists the virtual Vision AI sources, matching the web's visionai:<source> ids.
var Sources = []string{"general", "personas", "vehiculos", "lpr", "perimetro", "alarmas"}

// Layouts lists the accepted grid layouts.
var Layouts = []string{"1", "4", "9", "16", "focus"}

var (
	personObjects  = []string{"person", "face"}
	vehicleObjects = []string{"car", "motorcycle", "truck", "bus", "bicycle", "license_plate"}
)

// PresetConfig builds the session configuration for a virtual source and grid layout.
func PresetConfig(source, layout string) (VisionConfig, error) {
	if !slices.Contains(Layouts, layout) {
		return VisionConfig{}, &ValidationError{Msg: fmt.Sprintf("unknown layout %q", layout)}
	}
	cfg := DefaultVisionConfig(uuid.Nil)
	cfg.ID, cfg.Name, cfg.Layout, cfg.IsDefault = source, "Vision AI "+source, layout, false
	if layout == "focus" {
		cfg.Mode = "focus"
	}
	switch source {
	case "general":
		cfg.Objects = slices.Concat(personObjects, vehicleObjects)
	case "personas":
		cfg.Objects = slices.Clone(personObjects)
	case "vehiculos":
		cfg.Objects = slices.Clone(vehicleObjects)
	case "lpr":
		cfg.Mode, cfg.Objects, cfg.RequirePlate = "lpr", nil, true
	case "perimetro":
		cfg.Objects, cfg.RequireZone = []string{"person", "car", "motorcycle", "truck"}, true
	case "alarmas":
		cfg.Mode, cfg.Objects, cfg.RequireAlarm = "alarm", nil, true
	default:
		return VisionConfig{}, &ValidationError{Msg: fmt.Sprintf("unknown source %q", source)}
	}
	return cfg, nil
}
