package camconfig

import "testing"

func TestProvenanceEngineProfileOverride(t *testing.T) {
	m := Camera{
		Detection: Detection{Enabled: bp(false), Objects: []string{"person"}},
		Recording: Recording{Enabled: bp(true)},
		Sources:   []Source{{Name: "frigate", Status: SourceOK}},
	}
	profile := Patch{Detection: &DetectionPatch{Enabled: bp(true)}, Recording: &RecordingPatch{Enabled: bp(false)}}
	over := Patch{Recording: &RecordingPatch{Enabled: bp(true)}}
	got := ResolveProvenance(m, over, &ProfileRef{ID: "p1", Name: "Exterior"}, profile)
	if got["detection.enabled"].Source != ProvProfile || got["detection.enabled"].ProfileName != "Exterior" {
		t.Fatalf("detection.enabled = %+v", got["detection.enabled"])
	}
	if got["recording.enabled"].Source != ProvOverride {
		t.Fatalf("override must win: %+v", got["recording.enabled"])
	}
	if got["detection.objects"].Source != ProvEngine {
		t.Fatalf("objects = %+v", got["detection.objects"])
	}
	if _, ok := got["analytics.lpr"]; ok {
		t.Fatal("a value the engine did not report must not be invented")
	}
}

func TestProvenanceEngineNeedsReadableEngine(t *testing.T) {
	m := Camera{Detection: Detection{Enabled: bp(true)}}
	if got := ResolveProvenance(m, Patch{}, nil, Patch{}); len(got) != 0 {
		t.Fatalf("unreadable engine reported %v", got)
	}
}

func TestSubtractRemovesOverriddenLeaves(t *testing.T) {
	profile := Patch{Detection: &DetectionPatch{Enabled: bp(true)}, Recording: &RecordingPatch{Enabled: bp(true)},
		Streams: &StreamsPatch{Primary: &VideoPatch{Resolution: &VideoResolution{Width: 1920, Height: 1080}}}}
	over := Patch{Recording: &RecordingPatch{Enabled: bp(false)}}
	got := Subtract(profile, over)
	if got.Recording != nil || got.Detection == nil || got.Streams == nil || got.Streams.Primary.Resolution.Width != 1920 {
		t.Fatalf("subtract = %+v", got)
	}
}

func TestValidateProfilePatchRejectsSelectionAndUnknown(t *testing.T) {
	sel := []string{"vehicles"}
	if err := ValidateProfilePatch(Patch{Detection: &DetectionPatch{Selection: &sel}}); err == nil {
		t.Fatal("selection accepted in a profile")
	}
	if err := ValidateProfilePatch(Patch{}); err == nil {
		t.Fatal("empty profile patch accepted")
	}
}

func TestComputeStateUsesInheritedPatch(t *testing.T) {
	eff := Camera{Detection: Detection{Enabled: bp(false)}, Sources: []Source{{Name: "frigate", Status: SourceOK}}}
	st := ComputeState(Stored{Inherited: Patch{Detection: &DetectionPatch{Enabled: bp(true)}}, SyncState: SyncSynced}, eff)
	if len(st.Drift) != 1 || st.Drift[0].Path != "detection.enabled" {
		t.Fatalf("drift = %+v", st.Drift)
	}
}
