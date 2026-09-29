package config

import "testing"

func TestParseFeatures(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want Features
	}{
		{"empty is all off", "", Features{}},
		{"single flag", "persistentPlayers", Features{PersistentPlayers: true}},
		{"several flags with spaces and case", " persistentplayers , VideoSurfaceLayer ", Features{PersistentPlayers: true, VideoSurfaceLayer: true}},
		{"reserved flags", "adaptiveStreaming,streamPrewarming,seamlessQualitySwitch", Features{AdaptiveStreaming: true, StreamPrewarming: true, SeamlessQualitySwitch: true}},
		{"unknown names are ignored", "nope,persistentPlayers", Features{PersistentPlayers: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseFeatures(tc.raw); got != tc.want {
				t.Fatalf("ParseFeatures(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestLoadReadsFeaturesFromEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("OPENVMS_FEATURES", "persistentPlayers")
	c, err := Load("test")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Features.PersistentPlayers || c.Features.VideoSurfaceLayer {
		t.Fatalf("unexpected features: %+v", c.Features)
	}
}
