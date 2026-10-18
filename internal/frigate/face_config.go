package frigate

import (
	"context"
	"fmt"
	"net/http"
)

// Frigate's defaults for face_recognition (docs.frigate.video/configuration/face_recognition).
// GET /api/config normally lists every key; these fill in a server that omits some.
const (
	defaultFaceModelSize            = "small"
	defaultFaceDetectionThreshold   = 0.7
	defaultFaceRecognitionThreshold = 0.9
	defaultFaceUnknownScore         = 0.8
	defaultFaceMinFaces             = 1
	defaultFaceMinArea              = 750
	defaultFaceSaveAttempts         = 200
)

// FaceCameraSettings is the face_recognition block of one camera; nil means "not reported".
type FaceCameraSettings struct {
	Enabled *bool
	MinArea *int
}

// FaceRecognitionSettings is the running face_recognition configuration of a server.
type FaceRecognitionSettings struct {
	Enabled              bool
	ModelSize            string
	DetectionThreshold   float64
	RecognitionThreshold float64
	UnknownScore         float64
	MinFaces             int
	MinArea              int
	SaveAttempts         int
	BlurConfidenceFilter bool
	// Cameras is keyed by the Frigate camera name.
	Cameras map[string]FaceCameraSettings
}

// FaceRecognitionSettings reads the running face_recognition configuration (GET /api/config).
// What it returns is what Frigate runs now: a change saved with ApplyServerPatch only shows after
// Frigate restarts.
func (a *v017) FaceRecognitionSettings(ctx context.Context) (FaceRecognitionSettings, error) {
	var raw struct {
		FaceRecognition map[string]any `json:"face_recognition"`
		Cameras         map[string]struct {
			FaceRecognition map[string]any `json:"face_recognition"`
		} `json:"cameras"`
	}
	if err := a.c.getJSON(ctx, "/api/config", nil, &raw); err != nil {
		return FaceRecognitionSettings{}, err
	}
	g := raw.FaceRecognition
	out := FaceRecognitionSettings{
		Enabled:              boolOf(g["enabled"], false),
		ModelSize:            stringOf(g["model_size"], defaultFaceModelSize),
		DetectionThreshold:   floatOf(g["detection_threshold"], defaultFaceDetectionThreshold),
		RecognitionThreshold: floatOf(g["recognition_threshold"], defaultFaceRecognitionThreshold),
		UnknownScore:         floatOf(g["unknown_score"], defaultFaceUnknownScore),
		MinFaces:             int(floatOf(g["min_faces"], defaultFaceMinFaces)),
		MinArea:              int(floatOf(g["min_area"], defaultFaceMinArea)),
		SaveAttempts:         int(floatOf(g["save_attempts"], defaultFaceSaveAttempts)),
		BlurConfidenceFilter: boolOf(g["blur_confidence_filter"], true),
		Cameras:              make(map[string]FaceCameraSettings, len(raw.Cameras)),
	}
	for name, cam := range raw.Cameras {
		var cs FaceCameraSettings
		if v, ok := cam.FaceRecognition["enabled"].(bool); ok {
			cs.Enabled = &v
		}
		if v, ok := cam.FaceRecognition["min_area"].(float64); ok {
			n := int(v)
			cs.MinArea = &n
		}
		out.Cameras[name] = cs
	}
	return out, nil
}

// serverSections are the global sections ApplyServerPatch may write.
var serverSections = map[string]bool{"face_recognition": true}

// ApplyServerPatch merges value into one global config section through PUT /api/config/set.
// Only face_recognition is accepted; the change is saved and needs a Frigate restart.
func (a *v017) ApplyServerPatch(ctx context.Context, section string, value map[string]any) (SectionResult, error) {
	if !serverSections[section] {
		return SectionResult{}, &ConfigValidationError{Message: fmt.Sprintf("section %q cannot be changed server-wide from OpenVMS", section)}
	}
	if len(value) == 0 {
		return SectionResult{}, &ConfigValidationError{Message: "empty patch"}
	}
	if p, bad := FindRedacted(value); bad {
		return SectionResult{}, &ConfigValidationError{Message: fmt.Sprintf("%s holds a redacted value; redacted values are never written back", p)}
	}
	body := map[string]any{
		"config_data":      map[string]any{section: value},
		"requires_restart": 1,
	}
	if _, err := a.c.send(ctx, http.MethodPut, "/api/config/set", nil, body); err != nil {
		return SectionResult{}, asValidationError(err)
	}
	return SectionResult{Section: section, RequiresRestart: true}, nil
}

func boolOf(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

func stringOf(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func floatOf(v any, def float64) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return def
}
