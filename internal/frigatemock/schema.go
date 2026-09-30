package frigatemock

// configSchema is a small but structurally realistic slice of Frigate's pydantic schema:
// $defs with CameraConfig and the sections the editor cares about, enums and descriptions.
func configSchema() map[string]any {
	obj := func(desc string, props map[string]any) map[string]any {
		return map[string]any{"type": "object", "description": desc, "properties": props, "additionalProperties": false}
	}
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/$defs/" + name} }
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"title":   "FrigateConfig",
		"type":    "object",
		"properties": map[string]any{
			"cameras": map[string]any{"type": "object", "additionalProperties": ref("CameraConfig")},
			"mqtt":    ref("MqttConfig"),
		},
		"$defs": map[string]any{
			"CameraConfig": obj("Camera configuration.", map[string]any{
				"name":    map[string]any{"type": "string", "title": "Camera name"},
				"enabled": map[string]any{"type": "boolean", "default": true, "title": "Enabled", "description": "Enable or disable the camera."},
				"ffmpeg":  ref("CameraFfmpegConfig"),
				"detect":  ref("DetectConfig"),
				"record":  ref("RecordConfig"),
				"objects": ref("ObjectConfig"),
				"onvif":   ref("OnvifConfig"),
				"zones":   map[string]any{"type": "object", "additionalProperties": ref("ZoneConfig")},
			}),
			"CameraFfmpegConfig": obj("FFmpeg settings.", map[string]any{
				"inputs":       map[string]any{"type": "array", "minItems": 1, "items": ref("CameraInput")},
				"hwaccel_args": map[string]any{"type": "string", "description": "Hardware acceleration arguments."},
			}),
			"CameraInput": obj("One camera stream.", map[string]any{
				"path":  map[string]any{"type": "string", "description": "Stream URL."},
				"roles": map[string]any{"type": "array", "items": ref("CameraRoleEnum")},
			}),
			"CameraRoleEnum": map[string]any{"type": "string", "enum": []any{"audio", "detect", "record"}},
			"DetectConfig": obj("Object detection.", map[string]any{
				"enabled": map[string]any{"type": "boolean", "default": true, "description": "Enable object detection."},
				"width":   map[string]any{"type": "integer", "minimum": 1},
				"height":  map[string]any{"type": "integer", "minimum": 1},
				"fps":     map[string]any{"type": "integer", "minimum": 1, "default": 5, "description": "Detection frames per second."},
			}),
			"RecordConfig": obj("Recording.", map[string]any{
				"enabled": map[string]any{"type": "boolean", "default": false},
				"retain":  ref("RecordRetainConfig"),
			}),
			"RecordRetainConfig": obj("Retention.", map[string]any{
				"days": map[string]any{"type": "number", "minimum": 0, "default": 0, "description": "Days to keep recordings."},
			}),
			"ObjectConfig": obj("Tracked objects.", map[string]any{
				"track": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "default": []any{"person"}},
			}),
			"OnvifConfig": obj("ONVIF/PTZ.", map[string]any{
				"host":     map[string]any{"type": "string"},
				"port":     map[string]any{"type": "integer", "default": 8000},
				"user":     map[string]any{"type": "string"},
				"password": map[string]any{"type": "string"},
			}),
			"ZoneConfig": obj("Zone.", map[string]any{
				"coordinates": map[string]any{"type": "string", "description": "Relative coordinates x,y,..."},
			}),
			"MqttConfig": obj("MQTT.", map[string]any{
				"enabled":      map[string]any{"type": "boolean"},
				"topic_prefix": map[string]any{"type": "string"},
			}),
		},
	}
}
