package faces

import (
	"context"
	"errors"
	"image"
	"math"
)

// Detection is the detector's verdict on one crop. Landmarks are in the crop's own pixel
// coordinates, in ArcFace template order: the eye nearest the left edge, the other eye, the nose tip,
// then the left and right mouth corners. They are only meaningful when Found is true.
type Detection struct {
	Found     bool
	Score     float64
	Landmarks [5][2]float64
}

// FaceDetector looks for a face in a retained crop. The embedder uses it to keep crops that are not
// faces (empty frames, pavement, cars) out of the embedding and suggestion paths, and to align the
// ones that are before embedding them.
type FaceDetector interface {
	Detect(ctx context.Context, img image.Image) (Detection, error)
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

// yunetPlacement returns where a crop with bounds b sits on the YuNet canvas: the top-left corner and
// the scaled size, keeping the crop's aspect ratio inside a yunetFaceSize square centered on the canvas.
func yunetPlacement(b image.Rectangle) (x0, y0, w, h int) {
	w, h = yunetFaceSize, yunetFaceSize
	if b.Dx() > b.Dy() {
		h = max(1, yunetFaceSize*b.Dy()/b.Dx())
	} else if b.Dy() > b.Dx() {
		w = max(1, yunetFaceSize*b.Dx()/b.Dy())
	}
	return (yunetInputSize - w) / 2, (yunetInputSize - h) / 2, w, h
}

// preprocessYuNet scales the crop to yunetFaceSize (keeping its aspect ratio), centers it on a black
// yunetInputSize canvas, and returns BGR planes with raw 0-255 values, as YuNet expects.
func preprocessYuNet(src image.Image) ([]float32, error) {
	if src == nil || src.Bounds().Empty() {
		return nil, errors.New("face crop is empty")
	}
	b := src.Bounds()
	x0, y0, w, h := yunetPlacement(b)
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

// bestYuNetDetection returns the strongest anchor over every stride: its score, sqrt(clamp(cls) *
// clamp(obj)) because YuNet's cls and obj outputs are already probabilities, and its five landmarks
// on the canvas. Each kps anchor holds ten offsets (x, y per landmark) in units of the stride from
// the anchor's grid cell, with the person's right eye (the image's left side) first.
func bestYuNetDetection(cls, obj, kps [3][]float32) (score float64, canvas [5][2]float64, err error) {
	for s, stride := range yunetStrides {
		anchors := yunetAnchors(stride)
		if len(cls[s]) != anchors || len(obj[s]) != anchors || len(kps[s]) != anchors*10 {
			return 0, canvas, errors.New("face detector output has an unexpected size")
		}
		cols := yunetInputSize / stride
		for i := 0; i < anchors; i++ {
			c, o := clamp01(float64(cls[s][i])), clamp01(float64(obj[s][i]))
			if sc := math.Sqrt(c * o); sc > score {
				score = sc
				row, col := i/cols, i%cols
				for n := 0; n < 5; n++ {
					canvas[n] = [2]float64{
						(float64(col) + float64(kps[s][i*10+2*n])) * float64(stride),
						(float64(row) + float64(kps[s][i*10+2*n+1])) * float64(stride),
					}
				}
			}
		}
	}
	return score, canvas, nil
}

// toCropCoords maps landmarks from the YuNet canvas back to the pixel coordinates of the crop that
// was placed there with yunetPlacement.
func toCropCoords(b image.Rectangle, canvas [5][2]float64) [5][2]float64 {
	x0, y0, w, h := yunetPlacement(b)
	var out [5][2]float64
	for i, p := range canvas {
		out[i] = [2]float64{
			float64(b.Min.X) + (p[0]-float64(x0))*float64(b.Dx())/float64(w),
			float64(b.Min.Y) + (p[1]-float64(y0))*float64(b.Dy())/float64(h),
		}
	}
	return out
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Min(1, math.Max(0, v))
}
