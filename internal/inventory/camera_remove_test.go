package inventory

import "testing"

func TestRecordingRetainDaysReadsTheCameraThenTheGlobalSection(t *testing.T) {
	cfg := func(t *testing.T, raw string) (map[string]any, map[string]any) {
		t.Helper()
		parsed, cameras, err := parseCamerasConfig(raw)
		if err != nil {
			t.Fatal(err)
		}
		return parsed, cameras
	}
	days := func(p *int32) any {
		if p == nil {
			return nil
		}
		return *p
	}
	cases := []struct {
		name string
		yaml string
		want any
	}{
		{"camera retain days", "cameras:\n  a:\n    record:\n      retain:\n        days: 7\n", int32(7)},
		{"camera wins over the global section", "record:\n  retain:\n    days: 30\ncameras:\n  a:\n    record:\n      retain:\n        days: 3\n", int32(3)},
		{"global section when the camera has none", "record:\n  retain:\n    days: 14\ncameras:\n  a:\n    enabled: true\n", int32(14)},
		{"the longest of continuous, motion, alerts and detections", "cameras:\n  a:\n    record:\n      continuous:\n        days: 2\n      motion:\n        days: 5\n      alerts:\n        retain:\n          days: 30\n      detections:\n        retain:\n          days: 10\n", int32(30)},
		{"a fraction rounds up", "cameras:\n  a:\n    record:\n      retain:\n        days: 0.5\n", int32(1)},
		{"no window stated is unknown", "cameras:\n  a:\n    enabled: true\n", nil},
		{"a negative value is ignored", "cameras:\n  a:\n    record:\n      retain:\n        days: -1\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parsed, cameras := cfg(t, c.yaml)
			if got := days(recordingRetainDays(parsed, cameras["a"])); got != c.want {
				t.Fatalf("recordingRetainDays = %v, want %v", got, c.want)
			}
		})
	}
}
