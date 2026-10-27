package camconfig

import "github.com/jdolan-exalink/openvms/internal/onvif"

// Reasons the PTZ capabilities could not be read.
const (
	PTZReasonAgentOutdated = "agent_outdated"
	PTZReasonUnavailable   = "unavailable"
)

// PTZCapabilities is what the camera's PTZ node declares, so a client enables only what the device
// supports. Known is false when nothing could be read; then every flag is false and Reason says why.
type PTZCapabilities struct {
	Known      bool   `json:"known"`
	Reason     string `json:"reason,omitempty"`
	PresetEdit bool   `json:"preset_edit"`
	MaxPresets int    `json:"max_presets"`
	Home       bool   `json:"home"`
	HomeSet    bool   `json:"home_set"`
	PanTilt    bool   `json:"pan_tilt"`
	Zoom       bool   `json:"zoom"`
	// Focus is always false: moving focus needs the ONVIF Imaging service, which is not driven yet.
	Focus bool `json:"focus"`
}

// PTZCapabilitiesFromNodes derives the capabilities from the device's PTZ nodes. A device may list
// several nodes; the first one that stores presets decides (the profile-to-node binding is not read),
// otherwise the first node. No node at all reads as an unavailable PTZ.
func PTZCapabilitiesFromNodes(nodes []onvif.PTZNode) PTZCapabilities {
	if len(nodes) == 0 {
		return PTZCapabilities{Reason: PTZReasonUnavailable}
	}
	n := nodes[0]
	for _, c := range nodes {
		if c.MaximumPresets > 0 {
			n = c
			break
		}
	}
	return PTZCapabilities{
		Known:      true,
		PresetEdit: n.MaximumPresets > 0,
		MaxPresets: n.MaximumPresets,
		Home:       n.HomeSupported,
		HomeSet:    n.HomeSupported && !n.FixedHomePosition,
		PanTilt:    n.SupportsPanTilt,
		Zoom:       n.SupportsZoom,
	}
}

// PTZCapabilitiesUnknown is the answer when the nodes could not be read.
func PTZCapabilitiesUnknown(reason string) PTZCapabilities { return PTZCapabilities{Reason: reason} }
