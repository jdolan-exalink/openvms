package frigate

import (
	"context"
	"errors"
	"testing"
)

func TestFaceRecognitionSettingsReadsRunningConfigWithDefaults(t *testing.T) {
	ts, mock := startFaceMock(t, "0.17.2-mock", true)
	a := connectMock(t, ts)
	got, err := a.FaceRecognitionSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.ModelSize != "small" {
		t.Fatalf("enabled/model = %v %q", got.Enabled, got.ModelSize)
	}
	// The mock omits the thresholds: Frigate's documented defaults fill in.
	if got.DetectionThreshold != 0.7 || got.RecognitionThreshold != 0.9 || got.UnknownScore != 0.8 ||
		got.MinFaces != 1 || got.MinArea != 750 || got.SaveAttempts != 200 || !got.BlurConfidenceFilter {
		t.Fatalf("defaults = %+v", got)
	}
	if len(got.Cameras) == 0 {
		t.Fatal("no cameras reported")
	}
	_ = mock
}

func TestApplyServerPatchWritesGlobalSectionAndFlagsRestart(t *testing.T) {
	ts, mock := startFaceMock(t, "0.17.2-mock", false)
	a := connectMock(t, ts)
	ctx := context.Background()
	res, err := a.ApplyServerPatch(ctx, "face_recognition", map[string]any{"enabled": true, "model_size": "large", "recognition_threshold": 0.85})
	if err != nil {
		t.Fatal(err)
	}
	if res.Section != "face_recognition" || !res.RequiresRestart || res.AppliedLive {
		t.Fatalf("result = %+v, want saved with restart", res)
	}
	calls := mock.SetCalls()
	if len(calls) != 1 || calls[0].UpdateTopic != "" || calls[0].RequiresRestart == nil || *calls[0].RequiresRestart != 1 {
		t.Fatalf("set calls = %+v", calls)
	}
	got, err := a.FaceRecognitionSettings(ctx)
	if err != nil || !got.Enabled || got.ModelSize != "large" || got.RecognitionThreshold != 0.85 {
		t.Fatalf("read back = %+v, %v", got, err)
	}
}

func TestApplyServerPatchRejectsOtherSectionsEmptyAndRedacted(t *testing.T) {
	ts, _ := startFaceMock(t, "0.17.2-mock", true)
	a := connectMock(t, ts)
	var cv *ConfigValidationError
	for name, c := range map[string]struct {
		section string
		value   map[string]any
	}{
		"other section": {"mqtt", map[string]any{"host": "x"}},
		"empty":         {"face_recognition", nil},
		"redacted":      {"face_recognition", map[string]any{"device": "rtsp://*:*@h/x"}},
	} {
		if _, err := a.ApplyServerPatch(context.Background(), c.section, c.value); !errors.As(err, &cv) {
			t.Errorf("%s = %v, want ConfigValidationError", name, err)
		}
	}
}
