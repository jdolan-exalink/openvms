package inventory

import (
	"strings"
	"testing"
)

func TestNormalizePTZTour(t *testing.T) {
	ok := PTZTourInput{Name: " Ronda ", RepeatCount: 0, Steps: []PTZTourStep{{PresetToken: " 1 ", DwellSeconds: 10, Speed: 0.5}}}
	got, err := normalizePTZTour(ok)
	if err != nil || got.Name != "Ronda" || got.Steps[0].PresetToken != "1" {
		t.Fatalf("normalize = %+v, %v", got, err)
	}
	bad := map[string]PTZTourInput{
		"empty name":      {Name: " ", Steps: ok.Steps},
		"long name":       {Name: strings.Repeat("a", 129), Steps: ok.Steps},
		"no steps":        {Name: "x"},
		"too many steps":  {Name: "x", Steps: make([]PTZTourStep, 65)},
		"negative repeat": {Name: "x", Steps: ok.Steps, RepeatCount: -1},
		"huge repeat":     {Name: "x", Steps: ok.Steps, RepeatCount: 1001},
		"no preset":       {Name: "x", Steps: []PTZTourStep{{DwellSeconds: 1, Speed: 0.5}}},
		"long dwell":      {Name: "x", Steps: []PTZTourStep{{PresetToken: "1", DwellSeconds: 3601}}},
		"fast":            {Name: "x", Steps: []PTZTourStep{{PresetToken: "1", Speed: 1.5}}},
	}
	for name, in := range bad {
		if _, err := normalizePTZTour(in); err == nil {
			t.Errorf("%s: want validation error", name)
		}
	}
}
