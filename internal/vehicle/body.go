package vehicle

import (
	"image"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
)

// Body classes of the AutoLens EfficientNet-B2 crop classifier, in output order.
// The open-wheel class is a racing-car bucket from that dataset and is not a
// street type, so a win there is discarded.
const (
	bodyModelName       = "autolens-efficientnet-b2"
	bodyModelVersion    = "5"
	bodyTemperature     = 1.779860258102417
	bodyMinConfidence   = 0.45
	bodyShapeConfidence = 0.55
	bodySize            = 224
	bodyResize          = 256
)

var bodyLabels = []string{
	"suv",
	"van",
	"station_wagon",
	"micro",
	"open_wheel",
	"sedan",
	"hatchback",
	"pickup",
}

var bodyMean = [3]float32{0.4429, 0.4354, 0.4370}
var bodyStd = [3]float32{0.2456, 0.2421, 0.2449}

// leadingBody is the temperature-scaled top class, including classes that
// decideBody will not promote.
func leadingBody(logits []float32) (string, float32, bool) {
	if len(logits) != len(bodyLabels) {
		return "", 0, false
	}
	scaled := make([]float32, len(logits))
	max := float32(math.Inf(-1))
	for i, v := range logits {
		scaled[i] = v / bodyTemperature
		if scaled[i] > max {
			max = scaled[i]
		}
	}
	probs := make([]float64, len(scaled))
	var sum float64
	for i, v := range scaled {
		p := math.Exp(float64(v - max))
		probs[i] = p
		sum += p
	}
	best := 0
	for i := range probs {
		probs[i] /= sum
		if probs[i] > probs[best] {
			best = i
		}
	}
	return bodyLabels[best], float32(probs[best]), true
}

// decideBody turns raw logits into a street body type. Temperature scaling
// matches the calibration shipped with the model. A racing-car win, or a
// class below the floor, leaves the Frigate label in place.
func decideBody(logits []float32) (string, float32, bool) {
	label, conf, ok := leadingBody(logits)
	if !ok || label == "open_wheel" || conf < bodyMinConfidence {
		return "", 0, false
	}
	return label, conf, true
}

// bodyKeepsShape reports that the crop classifier was sure enough that a long
// box is still that body, and not an articulated truck.
func bodyKeepsShape(typ string, conf float32) bool {
	if conf < bodyShapeConfidence {
		return false
	}
	switch typ {
	case "sedan", "hatchback", "suv", "pickup", "van", "micro", "station_wagon":
		return true
	default:
		return false
	}
}

// applyBody keeps Frigate's motorcycle, bus and truck labels. A car crop can
// become a finer body type. A long box still becomes a truck with a trailer
// unless the crop classifier already named a street body with confidence.
func applyBody(frigateType string, frigateConf float32, bodyType string, bodyConf float32, attempted, accepted, rig bool) (string, float32, bool) {
	typ, conf := frigateType, frigateConf
	fromModel := false
	if frigateType == "car" && accepted {
		typ, conf = bodyType, bodyConf
		fromModel = true
	} else if frigateType == "car" && attempted {
		conf = 0
	}
	if rig && !bodyKeepsShape(typ, conf) {
		return "truck_trailer", 0.72, false
	}
	return typ, conf, fromModel
}

// bodyView is the vehicle interior inside Frigate's green rectangle.
func bodyView(img image.Image) (image.Image, bool) {
	box, ok := strokeBox(img, markerGreen)
	if !ok {
		return nil, false
	}
	r := insetRect(box)
	if r.Dx() < 16 || r.Dy() < 16 {
		return nil, false
	}
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), img, r.Min, draw.Src)
	return dst, true
}

// prepareBody resizes the short side to 256, center-crops 224, and writes
// NCHW float32 with the model's own mean and standard deviation.
func prepareBody(img image.Image) []float32 {
	return encodeBody(resizeShortSide(img))
}

func resizeShortSide(img image.Image) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := bodyResize, bodyResize
	if w < 1 || h < 1 {
		return image.NewRGBA(image.Rect(0, 0, bodySize, bodySize))
	}
	if w < h {
		nh = int(math.Round(float64(h) * float64(bodyResize) / float64(w)))
	} else if h < w {
		nw = int(math.Round(float64(w) * float64(bodyResize) / float64(h)))
	}
	if nw < bodySize {
		nw = bodySize
	}
	if nh < bodySize {
		nh = bodySize
	}
	resized := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.BiLinear.Scale(resized, resized.Bounds(), img, b, draw.Src, nil)
	crop := image.NewRGBA(image.Rect(0, 0, bodySize, bodySize))
	draw.Draw(crop, crop.Bounds(), resized, image.Pt((nw-bodySize)/2, (nh-bodySize)/2), draw.Src)
	return crop
}

func encodeBody(crop *image.RGBA) []float32 {
	out := make([]float32, 3*bodySize*bodySize)
	if crop == nil {
		return out
	}
	plane := bodySize * bodySize
	for y := 0; y < bodySize; y++ {
		row := crop.Pix[y*crop.Stride:]
		for x := 0; x < bodySize; x++ {
			i := y*bodySize + x
			for c := 0; c < 3; c++ {
				v := float32(row[x*4+c]) / 255
				out[c*plane+i] = (v - bodyMean[c]) / bodyStd[c]
			}
		}
	}
	return out
}
