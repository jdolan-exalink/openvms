package config

import "strings"

// Features holds the rollout flags of the Live View platform. All default to off so an
// existing install keeps its current behaviour until an operator opts in.
type Features struct {
	PersistentPlayers     bool
	VideoSurfaceLayer     bool
	AdaptiveStreaming     bool
	StreamPrewarming      bool
	SeamlessQualitySwitch bool
	Maps                  bool
}

// ParseFeatures reads a comma-separated list of flag names (case-insensitive). Unknown
// names are ignored so a newer env file never prevents an older binary from starting.
func ParseFeatures(raw string) Features {
	var f Features
	for _, name := range strings.Split(raw, ",") {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "persistentplayers":
			f.PersistentPlayers = true
		case "videosurfacelayer":
			f.VideoSurfaceLayer = true
		case "adaptivestreaming":
			f.AdaptiveStreaming = true
		case "streamprewarming":
			f.StreamPrewarming = true
		case "seamlessqualityswitch":
			f.SeamlessQualitySwitch = true
		case "maps":
			f.Maps = true
		}
	}
	return f
}

// EnabledList returns a slice of enabled feature names for protocol negotiation.
func (f Features) EnabledList() []string {
	var list []string
	if f.PersistentPlayers {
		list = append(list, "persistent_players")
	}
	if f.VideoSurfaceLayer {
		list = append(list, "video_surface_layer")
	}
	if f.AdaptiveStreaming {
		list = append(list, "adaptive_streaming")
	}
	if f.StreamPrewarming {
		list = append(list, "stream_prewarming")
	}
	if f.SeamlessQualitySwitch {
		list = append(list, "seamless_quality_switch")
	}
	if f.Maps {
		list = append(list, "maps")
	}
	return list
}
