package provision

import (
	"fmt"
	"strings"
)

// Variant is the Frigate compose file the installer applies.
type Variant string

const (
	VariantCoral    Variant = "coral"
	VariantTensorRT Variant = "tensorrt"
	VariantOpenVINO Variant = "openvino"
	VariantCPU      Variant = "cpu"
)

// Facts are the accelerators visible on a host.
type Facts struct {
	Coral    bool
	NVIDIA   bool
	OpenVINO bool
}

// Choose maps hardware to one Frigate variant.
// Coral wins, then NVIDIA, then OpenVINO, then CPU.
func Choose(f Facts) Variant {
	switch {
	case f.Coral:
		return VariantCoral
	case f.NVIDIA:
		return VariantTensorRT
	case f.OpenVINO:
		return VariantOpenVINO
	default:
		return VariantCPU
	}
}

// ParseFacts reads the single line printed by install.sh detect.
func ParseFacts(out string) (Facts, error) {
	var f Facts
	found := map[string]bool{}
	for _, field := range strings.Fields(out) {
		key, val, ok := strings.Cut(field, "=")
		if !ok || (val != "0" && val != "1") {
			continue
		}
		bit := val == "1"
		switch key {
		case "coral", "nvidia", "openvino":
			found[key] = true
			switch key {
			case "coral":
				f.Coral = bit
			case "nvidia":
				f.NVIDIA = bit
			case "openvino":
				f.OpenVINO = bit
			}
		}
	}
	if !found["coral"] || !found["nvidia"] || !found["openvino"] {
		return Facts{}, fmt.Errorf("hardware probe returned %q", tail(out, 200))
	}
	return f, nil
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// Scrub removes a secret from text that might be shown to an operator.
func Scrub(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "[redacted]")
}
