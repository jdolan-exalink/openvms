package faces

import (
	"context"
	"errors"
	"image"
	"math"
)

// FaceDetector answers whether a retained crop shows a face. The embedder uses it to keep crops
// that are not faces (empty frames, pavement, cars) out of the embedding and suggestion paths.
type FaceDetector interface {
	HasFace(ctx context.Context, img image.Image) (bool, error)
	Close() error
}

const (
	// yunetInputSize is the fixed square input of face_detection_yunet_2023mar.onnx.
	yunetInputSize = 640
	// yunetFaceSize is the side the crop is scaled to inside the input. Crops already are tight
	// face boxes, so filling the whole input would make the face larger than anything YuNet saw
	// in training; the crop is centered on a black canvas instead.
	yunetFaceSize = 160
	// DefaultFaceDetectionScore is the YuNet score (sqrt(cls*obj)) at which a crop counts as a face.
	// It is far below YuNet's usual 0.6-0.9 because the inputs are tight crops, and was set on 76
	// real crops from one deployment: with yunetFaceSize 160 empty frames, pavement and cars scored
	// at most 0.09 and every frontal face at least 0.20. Infrared profile faces score about 0.08 and
	// are treated as non-faces. It is not calibrated on consented data.
	DefaultFaceDetectionScore = 0.15
)

// yunetStrides are the three feature-map strides; each one contributes cls_<stride> and obj_<stride>
// outputs with (yunetInputSize/stride)^2 anchors.
var yunetStrides = [3]int{8, 16, 32}

func yunetAnchors(stride int) int { return (yunetInputSize / stride) * (yunetInputSize / stride) }

// preprocessYuNet scales the crop to yunetFaceSize (keeping its aspect ratio), centers it on a black
// yunetInputSize canvas, and returns BGR planes with raw 0-255 values, as YuNet expects.
func preprocessYuNet(src image.Image) ([]float32, error) {
	if src == nil || src.Bounds().Empty() {
		return nil, errors.New("face crop is empty")
	}
	b := src.Bounds()
	w, h := yunetFaceSize, yunetFaceSize
	if b.Dx() > b.Dy() {
		h = max(1, yunetFaceSize*b.Dy()/b.Dx())
	} else if b.Dy() > b.Dx() {
		w = max(1, yunetFaceSize*b.Dx()/b.Dy())
	}
	x0, y0 := (yunetInputSize-w)/2, (yunetInputSize-h)/2
	plane := yunetInputSize * yunetInputSize
	out := make([]float32, 3*plane)
	for y := 0; y < h; y++ {
		sy := b.Min.Y + y*b.Dy()/h
		for x := 0; x < w; x++ {
			sx := b.Min.X + x*b.Dx()/w
			r, g, bl, _ := src.At(sx, sy).RGBA()
			i := (y0+y)*yunetInputSize + x0 + x
			out[i] = float32(bl >> 8)
			out[plane+i] = float32(g >> 8)
			out[2*plane+i] = float32(r >> 8)
		}
	}
	return out, nil
}

// bestYuNetScore returns the highest face score over every anchor of every stride. YuNet's cls and
// obj outputs are already probabilities, so the score is sqrt(clamp(cls) * clamp(obj)).
func bestYuNetScore(cls, obj [3][]float32) (float64, error) {
	best := 0.0
	for s, stride := range yunetStrides {
		if len(cls[s]) != yunetAnchors(stride) || len(obj[s]) != yunetAnchors(stride) {
			return 0, errors.New("face detector output has an unexpected size")
		}
		for i := range cls[s] {
			c, o := clamp01(float64(cls[s][i])), clamp01(float64(obj[s][i]))
			if score := math.Sqrt(c * o); score > best {
				best = score
			}
		}
	}
	return best, nil
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Min(1, math.Max(0, v))
}
