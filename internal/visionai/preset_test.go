package visionai

import (
	"errors"
	"testing"
)

func TestPresetConfig(t *testing.T) {
	t.Run("every known source yields a config for the requested layout", func(t *testing.T) {
		for _, src := range Sources {
			cfg, err := PresetConfig(src, "9")
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", src, err)
			}
			if cfg.Layout != "9" || cfg.ID != src {
				t.Fatalf("%s: got id=%q layout=%q", src, cfg.ID, cfg.Layout)
			}
		}
	})

	t.Run("source specific filters", func(t *testing.T) {
		lpr, _ := PresetConfig("lpr", "4")
		if !lpr.RequirePlate {
			t.Fatalf("lpr preset must require a plate")
		}
		per, _ := PresetConfig("perimetro", "4")
		if !per.RequireZone || contains(per.Objects, "bicycle") {
			t.Fatalf("perimetro must require a zone and exclude bicycles: %+v", per)
		}
		al, _ := PresetConfig("alarmas", "4")
		if !al.RequireAlarm {
			t.Fatalf("alarmas preset must require an alarm")
		}
		pers, _ := PresetConfig("personas", "4")
		if !contains(pers.Objects, "person") || contains(pers.Objects, "car") {
			t.Fatalf("personas objects wrong: %v", pers.Objects)
		}
		veh, _ := PresetConfig("vehiculos", "4")
		if !contains(veh.Objects, "truck") || contains(veh.Objects, "person") {
			t.Fatalf("vehiculos objects wrong: %v", veh.Objects)
		}
	})

	t.Run("rejects unknown source and layout with a validation error", func(t *testing.T) {
		var ve *ValidationError
		if _, err := PresetConfig("nope", "4"); !errors.As(err, &ve) {
			t.Fatalf("unknown source: err = %v, want *ValidationError", err)
		}
		if _, err := PresetConfig("general", "7"); !errors.As(err, &ve) {
			t.Fatalf("unknown layout: err = %v, want *ValidationError", err)
		}
	})
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
