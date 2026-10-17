package inventory

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeCameraZone(t *testing.T) {
	tests := []struct {
		name      string
		zoneName  string
		points    []CameraZonePoint
		wantName  string
		wantPoint CameraZonePoint
		wantError bool
	}{
		{
			name:     "trims the Frigate zone name and rounds normalized points",
			zoneName: " entry ",
			points: []CameraZonePoint{
				{X: 0.12349, Y: 0.1}, {X: 0.9, Y: 0.1}, {X: 0.5, Y: 0.9},
			},
			wantName:  "entry",
			wantPoint: CameraZonePoint{X: 0.123, Y: 0.1},
		},
		{
			name:      "rejects coordinates outside normalized frame",
			zoneName:  "entry",
			points:    []CameraZonePoint{{X: -0.01, Y: 0.1}, {X: 0.9, Y: 0.1}, {X: 0.5, Y: 0.9}},
			wantError: true,
		},
		{
			name:      "rejects a degenerate polygon after normalization",
			zoneName:  "entry",
			points:    []CameraZonePoint{{X: 0.1, Y: 0.1}, {X: 0.1001, Y: 0.1}, {X: 0.1, Y: 0.1001}},
			wantError: true,
		},
		{
			name:      "rejects invalid Frigate zone names",
			zoneName:  "Front Door",
			points:    []CameraZonePoint{{X: 0.1, Y: 0.1}, {X: 0.9, Y: 0.1}, {X: 0.5, Y: 0.9}},
			wantError: true,
		},
		{
			name:      "rejects oversized Frigate zone names",
			zoneName:  strings.Repeat("a", 129),
			points:    []CameraZonePoint{{X: 0.1, Y: 0.1}, {X: 0.9, Y: 0.1}, {X: 0.5, Y: 0.9}},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeCameraZone(tt.zoneName, tt.points)
			if (err != nil) != tt.wantError {
				t.Fatalf("normalizeCameraZone() error = %v, wantError %v", err, tt.wantError)
			}
			if err != nil {
				return
			}
			if got.Name != tt.wantName {
				t.Errorf("name = %q, want %q", got.Name, tt.wantName)
			}
			if len(got.Points) == 0 || got.Points[0] != tt.wantPoint {
				t.Errorf("first normalized point = %#v, want %#v", got.Points, tt.wantPoint)
			}
		})
	}
}

func TestNormalizeZoneSettings(t *testing.T) {
	for _, key := range []string{"name", "coordinates"} {
		t.Run("rejects reserved "+key, func(t *testing.T) {
			settings := json.RawMessage(`{"` + key + `":"override"}`)
			if _, err := normalizeZoneSettings(settings); err == nil {
				t.Fatalf("normalizeZoneSettings accepted reserved key %q", key)
			}
		})
	}
}
