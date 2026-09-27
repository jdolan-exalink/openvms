package frigatemock

import (
	"fmt"
	"strings"
)

// ParseCameras reads the MOCK_CAMERAS format: name[+lpr][:zone|zone], comma separated.
func ParseCameras(spec string) ([]Camera, error) {
	var out []Camera
	for _, raw := range strings.Split(spec, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var c Camera
		name, zones, _ := strings.Cut(raw, ":")
		if base, ok := strings.CutSuffix(name, "+lpr"); ok {
			name, c.LPR = base, true
		}
		if name == "" {
			return nil, fmt.Errorf("camera with empty name in %q", raw)
		}
		c.Name = name
		if zones != "" {
			c.Zones = strings.Split(zones, "|")
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no cameras in %q", spec)
	}
	return out, nil
}
