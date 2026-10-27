package camconfig

import (
	"testing"

	"github.com/jdolan-exalink/openvms/internal/onvif"
)

func TestPTZCapabilitiesFollowTheNode(t *testing.T) {
	full := PTZCapabilitiesFromNodes([]onvif.PTZNode{{MaximumPresets: 64, HomeSupported: true, SupportsPanTilt: true, SupportsZoom: true}})
	if !full.Known || !full.PresetEdit || full.MaxPresets != 64 || !full.Home || !full.HomeSet || !full.PanTilt || !full.Zoom || full.Focus {
		t.Fatalf("full: %+v", full)
	}
	fixed := PTZCapabilitiesFromNodes([]onvif.PTZNode{{MaximumPresets: 8, HomeSupported: true, FixedHomePosition: true}})
	if !fixed.Home || fixed.HomeSet {
		t.Fatalf("a fixed home can be gone to but not saved: %+v", fixed)
	}
	none := PTZCapabilitiesFromNodes([]onvif.PTZNode{{MaximumPresets: 0}})
	if !none.Known || none.PresetEdit || none.Home || none.HomeSet {
		t.Fatalf("a node without presets or home offers neither: %+v", none)
	}
	pick := PTZCapabilitiesFromNodes([]onvif.PTZNode{{Token: "a"}, {Token: "b", MaximumPresets: 10, HomeSupported: true}})
	if !pick.PresetEdit || !pick.Home {
		t.Fatalf("the node that stores presets decides: %+v", pick)
	}
	if empty := PTZCapabilitiesFromNodes(nil); empty.Known || empty.Reason != PTZReasonUnavailable {
		t.Fatalf("empty: %+v", empty)
	}
	if u := PTZCapabilitiesUnknown(PTZReasonAgentOutdated); u.Known || u.PresetEdit || u.Reason != "agent_outdated" {
		t.Fatalf("unknown: %+v", u)
	}
}
