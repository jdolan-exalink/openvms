package maps

import (
	"bytes"
	"image/png"
)

const MaxPlanBytes = 20 << 20
const MaxPlanSide = 8192
const MaxPlanPixels = 16_000_000

// Decode independently of MIME/client dimensions, then discard ancillary and trailing content.
func canonicalPlanPNG(data []byte) ([]byte, int32, int32, error) {
	invalid := func() ([]byte, int32, int32, error) {
		return nil, 0, 0, &ValidationError{Msg: "plan must be a valid PNG within 20 MiB, 8192 pixels per side and 16 million pixels"}
	}
	if len(data) == 0 || len(data) > MaxPlanBytes {
		return invalid()
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxPlanSide || cfg.Height > MaxPlanSide || int64(cfg.Width)*int64(cfg.Height) > MaxPlanPixels {
		return invalid()
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return invalid()
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, 0, 0, err
	}
	if out.Len() > MaxPlanBytes {
		return invalid()
	}
	return out.Bytes(), int32(cfg.Width), int32(cfg.Height), nil
}
