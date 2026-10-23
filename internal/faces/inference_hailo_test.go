//go:build cgo

package faces

import (
	"image"
	"image/color"
	"testing"
)

func TestPreprocessHailoRGB112KeepsRawRGBBytes(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 255, G: 127, B: 0, A: 255})
	got, err := preprocessHailoRGB112(img)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != faceModelInputSize*faceModelInputSize*3 {
		t.Fatalf("input length = %d", len(got))
	}
	if got[0] != 255 || got[1] != 127 || got[2] != 0 {
		t.Fatalf("first pixel = %v, want unnormalized RGB bytes [255 127 0]", got[:3])
	}
}

func TestValidateHailoContractRequiresOneRGBInputAnd512Vector(t *testing.T) {
	tests := []struct {
		name       string
		inputSizes []int
		outputs    []string
		outputName string
		wantErr    bool
	}{
		{name: "supported MobileFaceNet HEF", inputSizes: []int{112 * 112 * 3}, outputs: []string{"arcface_mobilefacenet/fc1"}, outputName: "arcface_mobilefacenet/fc1"},
		{name: "wrong input size", inputSizes: []int{224 * 224 * 3}, outputs: []string{"embedding"}, wantErr: true},
		{name: "multiple inputs", inputSizes: []int{112 * 112 * 3, 4}, outputs: []string{"embedding"}, wantErr: true},
		{name: "multiple outputs", inputSizes: []int{112 * 112 * 3}, outputs: []string{"embedding", "quality"}, wantErr: true},
		{name: "requested output missing", inputSizes: []int{112 * 112 * 3}, outputs: []string{"embedding"}, outputName: "wrong", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateHailoContract(tt.inputSizes, tt.outputs, tt.outputName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateHailoContract() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
