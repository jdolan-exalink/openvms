//go:build cgo

package faces

import (
	"errors"
	"fmt"
	"image"
)

func validateHailoContract(inputSizes []int, outputNames []string, requestedOutput string) (string, error) {
	if len(inputSizes) != 1 || inputSizes[0] != faceModelInputSize*faceModelInputSize*3 {
		return "", fmt.Errorf("Hailo-8 HEF must have one 112x112 RGB input (%d bytes); got %v", faceModelInputSize*faceModelInputSize*3, inputSizes)
	}
	if len(outputNames) != 1 {
		return "", fmt.Errorf("Hailo-8 HEF must have one embedding output; got %d outputs", len(outputNames))
	}
	if requestedOutput != "" && requestedOutput != outputNames[0] {
		return "", fmt.Errorf("Hailo-8 HEF has output %q, requested %q", outputNames[0], requestedOutput)
	}
	return outputNames[0], nil
}

// preprocessHailoRGB112 sends raw RGB bytes. The compatible Model Zoo HEF
// includes the [-1, 1] normalization layer, so the host must not normalize it.
func preprocessHailoRGB112(src image.Image) ([]byte, error) {
	if src == nil || src.Bounds().Empty() {
		return nil, errors.New("face crop is empty")
	}
	bounds := src.Bounds()
	out := make([]byte, faceModelInputSize*faceModelInputSize*3)
	for y := 0; y < faceModelInputSize; y++ {
		sy := bounds.Min.Y + y*bounds.Dy()/faceModelInputSize
		for x := 0; x < faceModelInputSize; x++ {
			sx := bounds.Min.X + x*bounds.Dx()/faceModelInputSize
			r, g, b, _ := src.At(sx, sy).RGBA()
			i := (y*faceModelInputSize + x) * 3
			out[i], out[i+1], out[i+2] = byte((r>>8)&0xff), byte((g>>8)&0xff), byte((b>>8)&0xff)
		}
	}
	return out, nil
}
