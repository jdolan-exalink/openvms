package faces

import (
	"errors"
	"image"
	"image/color"
	"math"
)

// faceModelInputSize is the side of the square image ArcFace models take. It lives here, not with
// the cgo-only runtime, because aligning does not need cgo.
const faceModelInputSize = 112

// arcFaceTemplate is the standard 112x112 landmark layout ArcFace models are trained on: the eye
// nearest the image's left edge, the other eye, the nose tip, and the left and right mouth corners.
var arcFaceTemplate = [5][2]float64{
	{38.2946, 51.6963}, {73.5318, 51.5014}, {56.0252, 71.7366}, {41.5493, 92.3655}, {70.7299, 92.2041},
}

// similarity is a 2x3 transform [a b tx; c d ty] (rotation, uniform scale, translation).
type similarity [6]float64

func (m similarity) scale() float64 { return math.Hypot(m[0], m[3]) }

// estimateSimilarity returns the least-squares similarity transform (Umeyama) that maps the detected
// landmarks onto dst. It fails on degenerate input, such as all points at the same place.
func estimateSimilarity(src, dst [5][2]float64) (similarity, error) {
	var mx, my, nx, ny float64
	for i := range src {
		mx += src[i][0]
		my += src[i][1]
		nx += dst[i][0]
		ny += dst[i][1]
	}
	n := float64(len(src))
	mx, my, nx, ny = mx/n, my/n, nx/n, ny/n
	var varSrc, dot, cross float64
	for i := range src {
		sx, sy := src[i][0]-mx, src[i][1]-my
		dx, dy := dst[i][0]-nx, dst[i][1]-ny
		varSrc += sx*sx + sy*sy
		dot += sx*dx + sy*dy
		cross += sx*dy - sy*dx
	}
	norm := math.Hypot(dot, cross)
	if varSrc == 0 || norm == 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return similarity{}, errors.New("face landmarks are degenerate")
	}
	cosT, sinT, scale := dot/norm, cross/norm, norm/varSrc
	a, b := scale*cosT, -scale*sinT
	c, d := scale*sinT, scale*cosT
	return similarity{a, b, nx - (a*mx + b*my), c, d, ny - (c*mx + d*my)}, nil
}

// alignFace112 warps the crop so its landmarks land on arcFaceTemplate, producing the 112x112 input
// ArcFace expects. Pixels outside the crop repeat its border. Landmarks are in the image's own
// coordinates (the same ones At uses), so a crop whose bounds do not start at (0, 0) works too.
func alignFace112(src image.Image, landmarks [5][2]float64) (image.Image, error) {
	if src == nil || src.Bounds().Empty() {
		return nil, errors.New("face crop is empty")
	}
	m, err := estimateSimilarity(landmarks, arcFaceTemplate)
	if err != nil {
		return nil, err
	}
	// A real face crop never needs more than a few times magnification or reduction.
	if s := m.scale(); s < 0.05 || s > 20 {
		return nil, errors.New("face landmarks give an implausible scale")
	}
	det := m[0]*m[4] - m[1]*m[3]
	i00, i01, i10, i11 := m[4]/det, -m[1]/det, -m[3]/det, m[0]/det
	b := src.Bounds()
	at := func(x, y int) (r, g, bl float64) {
		x = min(max(x, b.Min.X), b.Max.X-1)
		y = min(max(y, b.Min.Y), b.Max.Y-1)
		cr, cg, cb, _ := src.At(x, y).RGBA()
		return float64(cr >> 8), float64(cg >> 8), float64(cb >> 8)
	}
	out := image.NewRGBA(image.Rect(0, 0, faceModelInputSize, faceModelInputSize))
	for v := 0; v < faceModelInputSize; v++ {
		for u := 0; u < faceModelInputSize; u++ {
			du, dv := float64(u)-m[2], float64(v)-m[5]
			sx, sy := i00*du+i01*dv, i10*du+i11*dv // landmarks are in the image's own coordinates, Bounds().Min included
			x0, y0 := int(math.Floor(sx)), int(math.Floor(sy))
			fx, fy := sx-float64(x0), sy-float64(y0)
			var rgb [3]float64
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					w := (1 - math.Abs(float64(dx)-fx)) * (1 - math.Abs(float64(dy)-fy))
					r, g, bl := at(x0+dx, y0+dy)
					rgb[0] += w * r
					rgb[1] += w * g
					rgb[2] += w * bl
				}
			}
			out.Set(u, v, color.RGBA{R: uint8(rgb[0] + 0.5), G: uint8(rgb[1] + 0.5), B: uint8(rgb[2] + 0.5), A: 255}) //nolint:gosec // values are bilinear means of bytes, within 0-255.
		}
	}
	return out, nil
}
