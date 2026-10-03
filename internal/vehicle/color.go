package vehicle

import (
	"image"
	"math"
	"sort"
)

// Color is one reading of the dominant paint or clothing color.
type Color struct {
	Name       string
	Confidence float32
	Quality    string
}

const (
	colorMinConfidence = 0.55
	qualityGood        = "good"
	qualityMedium      = "medium"
	qualityLow         = "low"
)

// ClassifyColor reads the vehicle body. The Frigate box is used when it is
// burned into the snapshot. Otherwise box, when non-empty, is the object
// rectangle in image pixels (mapped from the event region).
func ClassifyColor(img image.Image) Color {
	return ClassifyColorBox(img, image.Rectangle{})
}

// ClassifyColorBox is ClassifyColor with an optional object rectangle.
func ClassifyColorBox(img image.Image, box image.Rectangle) Color {
	region, boxed := objectRegion(img, box, markerGreen)
	return voteColor(img, region, boxed)
}

// ClassifyRig reports a truck with a trailer when the detection box is much
// longer than a car, and reads the cab and the trailer separately. The cab is
// the end with the windows. Frigate's car label does not say this on its own.
func ClassifyRig(img image.Image) (cab, trailer Color, ok bool) {
	box, found := strokeBox(img, markerGreen)
	if !found {
		return Color{}, Color{}, false
	}
	w, h := box.Dx(), box.Dy()
	long, short := w, h
	horizontal := true
	if h > w {
		long, short = h, w
		horizontal = false
	}
	if short < 12 || long*10 < short*23 {
		return Color{}, Color{}, false
	}
	region := insetRect(box)
	start := sliceAlong(region, horizontal, 0, 32)
	end := sliceAlong(region, horizontal, 68, 100)
	cabSpan, trailerSpan := start, sliceAlong(region, horizontal, 32, 100)
	if lightnessSpread(img, end) > lightnessSpread(img, start) {
		cabSpan = end
		trailerSpan = sliceAlong(region, horizontal, 0, 68)
	}
	return voteColor(img, cabSpan, true), voteColor(img, trailerSpan, true), true
}

func sliceAlong(r image.Rectangle, horizontal bool, start, end int) image.Rectangle {
	if horizontal {
		w := r.Dx()
		return image.Rect(r.Min.X+w*start/100, r.Min.Y, r.Min.X+w*end/100, r.Max.Y)
	}
	h := r.Dy()
	return image.Rect(r.Min.X, r.Min.Y+h*start/100, r.Max.X, r.Min.Y+h*end/100)
}

func lightnessSpread(img image.Image, region image.Rectangle) float64 {
	if region.Dx() < 4 || region.Dy() < 4 {
		return 0
	}
	var n int
	var sum, sum2 float64
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if markerGreen(r, g, b) || markerYellow(r, g, b) {
				continue
			}
			L, _, _ := rgbToLab(float64(r)/65535, float64(g)/65535, float64(b)/65535)
			sum += L
			sum2 += L * L
			n++
		}
	}
	if n < 8 {
		return 0
	}
	mean := sum / float64(n)
	v := sum2/float64(n) - mean*mean
	if v < 0 {
		return 0
	}
	return math.Sqrt(v)
}

// Clothing is the upper and lower garment of one person.
type Clothing struct {
	Upper Color
	Lower Color
}

// ClassifyClothing splits a person into shirt and pants. The head and the
// feet are left out so the ground and the face do not vote.
func ClassifyClothing(img image.Image) Clothing {
	return ClassifyClothingBox(img, image.Rectangle{})
}

// ClassifyClothingBox is ClassifyClothing with an optional object rectangle.
func ClassifyClothingBox(img image.Image, box image.Rectangle) Clothing {
	unknown := Color{Name: "unknown", Quality: qualityLow}
	region, ok := personRegion(img, box)
	if !ok {
		return Clothing{Upper: unknown, Lower: unknown}
	}
	region = torsoColumn(region)
	h := region.Dy()
	body0 := region.Min.Y + h*22/100
	body1 := region.Max.Y - h*12/100
	if body1-body0 < 8 {
		body0, body1 = region.Min.Y, region.Max.Y
	}
	mid := body0 + (body1-body0)/2
	upper := image.Rect(region.Min.X, body0, region.Max.X, mid)
	lower := image.Rect(region.Min.X, mid, region.Max.X, body1)
	return Clothing{Upper: voteGarment(img, upper), Lower: voteGarment(img, lower)}
}

// personRegion is the person rectangle. Helvecia draws it blue and Casa draws
// it yellow. Without one of those strokes, or an explicit box, the frame center
// is the street and must not be read as clothing.
func personRegion(img image.Image, hint image.Rectangle) (image.Rectangle, bool) {
	if r, ok := strokeBox(img, markerYellow); ok {
		return insetRect(r), true
	}
	if r, ok := strokeBox(img, markerBlue); ok {
		return insetRect(r), true
	}
	if hint.Dx() >= 8 && hint.Dy() >= 8 {
		h := hint.Intersect(img.Bounds())
		if h.Dx() >= 8 && h.Dy() >= 8 {
			return insetRect(h), true
		}
	}
	return image.Rectangle{}, false
}

// torsoColumn keeps the middle of the box. The sides are usually sidewalk.
func torsoColumn(r image.Rectangle) image.Rectangle {
	cut := r.Dx() * 30 / 100
	out := image.Rect(r.Min.X+cut, r.Min.Y, r.Max.X-cut, r.Max.Y)
	if out.Dx() < 6 {
		return r
	}
	return out
}

// RegionBox maps a Frigate box through its detector region onto a snapshot.
// Both are [x, y, width, height] normalized to the frame. The snapshot is the
// region resized to the image.
func RegionBox(img image.Image, region, box [4]float64) image.Rectangle {
	b := img.Bounds()
	if region[2] <= 0.01 || region[3] <= 0.01 || box[2] <= 0 || box[3] <= 0 {
		return image.Rectangle{}
	}
	rx := (box[0] - region[0]) / region[2]
	ry := (box[1] - region[1]) / region[3]
	rw := box[2] / region[2]
	rh := box[3] / region[3]
	x0 := b.Min.X + int(math.Round(rx*float64(b.Dx())))
	y0 := b.Min.Y + int(math.Round(ry*float64(b.Dy())))
	x1 := b.Min.X + int(math.Round((rx+rw)*float64(b.Dx())))
	y1 := b.Min.Y + int(math.Round((ry+rh)*float64(b.Dy())))
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	if x1-x0 < 8 || y1-y0 < 8 {
		return image.Rectangle{}
	}
	return image.Rect(x0, y0, x1, y1)
}

func objectRegion(img image.Image, hint image.Rectangle, marker func(uint32, uint32, uint32) bool) (image.Rectangle, bool) {
	if r, ok := strokeBox(img, marker); ok {
		return insetRect(r), true
	}
	if hint.Dx() >= 8 && hint.Dy() >= 8 {
		return insetRect(hint.Intersect(img.Bounds())), true
	}
	b := img.Bounds()
	return image.Rect(b.Min.X+b.Dx()/5, b.Min.Y+b.Dy()/5, b.Max.X-b.Dx()/5, b.Max.Y-b.Dy()/5), false
}

func insetRect(r image.Rectangle) image.Rectangle {
	w, h := r.Dx(), r.Dy()
	ix, iy := w*10/100, h*10/100
	if ix < 2 {
		ix = 2
	}
	if iy < 2 {
		iy = 2
	}
	top := iy + h*6/100
	out := image.Rect(r.Min.X+ix, r.Min.Y+top, r.Max.X-ix, r.Max.Y-iy)
	if out.Dx() < 4 || out.Dy() < 4 {
		return r
	}
	return out
}

// strokeBox is the hollow rectangle Frigate draws. The filled label under the
// box is ignored, and so are trees, which do not form two edge clusters.
func strokeBox(img image.Image, marker func(uint32, uint32, uint32) bool) (image.Rectangle, bool) {
	b := img.Bounds()
	type edge struct{ y, left, right int }
	var edges []edge
	for y := b.Min.Y; y < b.Max.Y; y++ {
		var xs []int
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if marker(r, g, bl) {
				xs = append(xs, x)
			}
		}
		if len(xs) < 2 {
			continue
		}
		left, right := xs[0], xs[len(xs)-1]
		if right-left < 12 {
			continue
		}
		leftN, rightN, mid := 0, 0, 0
		for _, x := range xs {
			switch {
			case x-left <= 5:
				leftN++
			case right-x <= 5:
				rightN++
			default:
				mid++
			}
		}
		if leftN == 0 || rightN == 0 || mid > leftN+rightN {
			continue
		}
		edges = append(edges, edge{y, left, right})
	}
	if len(edges) < 8 {
		return image.Rectangle{}, false
	}
	// Stray rows above the rectangle must not stretch the box up into the street.
	run0, runN := 0, 1
	best0, best1, bestN := 0, 0, 1
	for i := 1; i < len(edges); i++ {
		if edges[i].y-edges[i-1].y <= 4 {
			runN++
			continue
		}
		if runN > bestN {
			best0, best1, bestN = run0, i-1, runN
		}
		run0, runN = i, 1
	}
	if runN > bestN {
		best0, best1, bestN = run0, len(edges)-1, runN
	}
	if bestN < 8 {
		return image.Rectangle{}, false
	}
	span := edges[best0 : best1+1]
	lefts := make([]int, len(span))
	rights := make([]int, len(span))
	for i, e := range span {
		lefts[i] = e.left
		rights[i] = e.right
	}
	sort.Ints(lefts)
	sort.Ints(rights)
	x0 := lefts[len(lefts)/2]
	x1 := rights[len(rights)/2]
	y0, y1 := span[0].y, span[len(span)-1].y
	if x1-x0 < 12 || y1-y0 < 12 {
		return image.Rectangle{}, false
	}
	return image.Rect(x0, y0, x1+1, y1+1), true
}

func voteColor(img image.Image, region image.Rectangle, boxed bool) Color {
	if region.Empty() {
		return Color{Name: "unknown", Quality: qualityLow}
	}
	step := 1
	if region.Dx()*region.Dy() > 40000 {
		step = 2
	}
	counts := map[string]int{}
	var n, neutralN int
	var sumL, sumC, neutralL float64
	for y := region.Min.Y; y < region.Max.Y; y += step {
		for x := region.Min.X; x < region.Max.X; x += step {
			r16, g16, b16, _ := img.At(x, y).RGBA()
			L, a, b := rgbToLab(float64(r16)/65535, float64(g16)/65535, float64(b16)/65535)
			chroma := math.Hypot(a, b)
			sumL += L
			sumC += chroma
			n++
			if L < 6 || L > 98 || markerGreen(r16, g16, b16) || markerYellow(r16, g16, b16) {
				continue
			}
			name := paintName(L, a, b, chroma)
			counts[name]++
			if name == "black" || name == "white" || name == "gray" || name == "silver" {
				neutralN++
				neutralL += L
			}
		}
	}
	if n == 0 {
		return Color{Name: "unknown", Quality: qualityLow}
	}
	meanL := sumL / float64(n)
	meanC := sumC / float64(n)
	// A white body is a light patch standing above the pavement. The mean of the
	// whole frame is the road, so the patch has to be named on its own.
	paint, painted := brightPaint(img, region)
	if !boxed && meanC < 3.5 && meanL > 18 && meanL < 78 {
		if painted {
			return paint
		}
		return Color{Name: "unknown", Confidence: 0.2, Quality: qualityLow}
	}
	total := 0
	for _, c := range counts {
		total += c
	}
	if total == 0 {
		return Color{Name: "unknown", Quality: qualityLow}
	}
	best, bestN := "", 0
	for name, c := range counts {
		if name == "other" {
			continue
		}
		if c > bestN {
			best, bestN = name, c
		}
	}
	chromaticN := bestN
	if best == "black" || best == "white" || best == "gray" || best == "silver" {
		chromaticN = 0
		for name, c := range counts {
			if name == "other" || name == "black" || name == "white" || name == "gray" || name == "silver" {
				continue
			}
			if c > chromaticN {
				chromaticN = c
				best = name
			}
		}
	}
	// The green detection box and its label survive JPEG as a slightly impure
	// green. A real green body fills most of the interior; a thin box does not.
	if best == "green" && counts["green"]*100 < total*55 {
		chromaticN = 0
		best = ""
		for name, c := range counts {
			if name == "green" || name == "other" || name == "black" || name == "white" || name == "gray" || name == "silver" {
				continue
			}
			if c > chromaticN {
				chromaticN = c
				best = name
			}
		}
	}
	// A black or silver body loses to blue window reflections when each bright
	// pixel is weighted by chroma. Count pixels, and keep the neutral body when
	// it is the majority.
	if neutralN*100 >= total*35 && (chromaticN == 0 || chromaticN*100 < total*35) {
		if painted {
			return paint
		}
		name := neutralName(neutralL / float64(neutralN))
		conf := float32(neutralN) / float32(total)
		quality := qualityMedium
		if conf >= 0.7 {
			quality = qualityGood
		}
		return Color{Name: name, Confidence: conf, Quality: quality}
	}
	if best == "" || chromaticN == 0 {
		if painted {
			return paint
		}
		if neutralN == 0 {
			return Color{Name: "unknown", Quality: qualityLow}
		}
		name := neutralName(neutralL / float64(neutralN))
		return Color{Name: name, Confidence: float32(neutralN) / float32(total), Quality: qualityMedium}
	}
	conf := float32(chromaticN) / float32(total)
	floor := float32(0.40)
	if boxed {
		floor = 0.30
	}
	if conf < floor {
		return Color{Name: "unknown", Confidence: conf, Quality: qualityLow}
	}
	quality := qualityGood
	if conf < 0.55 || meanL < 12 || meanL > 94 {
		quality = qualityMedium
	}
	return Color{Name: best, Confidence: conf, Quality: quality}
}

// brightPaint is the white body when it is a light patch above the pavement.
// The mean of the frame is the road, and a white vehicle in these cameras
// measures around L 75, not the L 86 of a flat white swatch.
func brightPaint(img image.Image, region image.Rectangle) (Color, bool) {
	if region.Empty() {
		return Color{}, false
	}
	var hist [100]int
	neutral := 0
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if markerGreen(r, g, b) || markerYellow(r, g, b) {
				continue
			}
			L, a, bv := rgbToLab(float64(r)/65535, float64(g)/65535, float64(b)/65535)
			if L < 6 || L > 98 || math.Hypot(a, bv) >= 12 {
				continue
			}
			hist[int(L)]++
			neutral++
		}
	}
	if neutral < 40 {
		return Color{}, false
	}
	var bins [13]int
	for L, c := range hist {
		bins[L/8] += c
	}
	mode, modeN := 0, 0
	for i, c := range bins {
		if c > modeN {
			mode, modeN = i, c
		}
	}
	// Windows and tires pull the average down. When the lighter mass of the
	// same region is already white, that mass is the body.
	if p, mean := histPercentile(hist, neutral, 60); p >= 74 && float64(p) >= mean+15 {
		return Color{Name: "white", Confidence: 0.72, Quality: qualityGood}, true
	}
	floor := float64(mode*8 + 4 + 10)
	if floor < 68 {
		floor = 68
	}
	bw, bh := region.Dx(), region.Dy()
	mask := make([]byte, bw*bh)
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			r, g, b, _ := img.At(region.Min.X+x, region.Min.Y+y).RGBA()
			if markerGreen(r, g, b) || markerYellow(r, g, b) {
				continue
			}
			L, a, bv := rgbToLab(float64(r)/65535, float64(g)/65535, float64(b)/65535)
			if L >= floor && L <= 98 && math.Hypot(a, bv) < 14 {
				mask[y*bw+x] = 1
			}
		}
	}
	seen := make([]byte, len(mask))
	bestN := 0
	median := 0.0
	var stack []int
	for i := range mask {
		if mask[i] == 0 || seen[i] == 1 {
			continue
		}
		var comp []float64
		stack = stack[:0]
		stack = append(stack, i)
		seen[i] = 1
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := p%bw, p/bw
			r, g, b, _ := img.At(region.Min.X+x, region.Min.Y+y).RGBA()
			L, _, _ := rgbToLab(float64(r)/65535, float64(g)/65535, float64(b)/65535)
			comp = append(comp, L)
			if x > 0 {
				tryBlob(&stack, seen, mask, p-1)
			}
			if x+1 < bw {
				tryBlob(&stack, seen, mask, p+1)
			}
			if y > 0 {
				tryBlob(&stack, seen, mask, p-bw)
			}
			if y+1 < bh {
				tryBlob(&stack, seen, mask, p+bw)
			}
		}
		if len(comp) > bestN {
			bestN = len(comp)
			sort.Float64s(comp)
			median = comp[len(comp)/2]
		}
	}
	area := bw * bh
	if area < 1 || bestN*1000 < area*12 || median < 74 {
		return Color{}, false
	}
	quality := qualityMedium
	if bestN*100 >= area*3 {
		quality = qualityGood
	}
	return Color{Name: "white", Confidence: 0.72, Quality: quality}, true
}

func histPercentile(hist [100]int, total, p int) (int, float64) {
	want := total * p / 100
	seen := 0
	sum := 0
	for L, c := range hist {
		sum += L * c
	}
	mean := float64(sum) / float64(total)
	for L, c := range hist {
		seen += c
		if seen >= want {
			return L, mean
		}
	}
	return 0, mean
}

func tryBlob(stack *[]int, seen, mask []byte, q int) {
	if seen[q] == 1 || mask[q] == 0 {
		return
	}
	seen[q] = 1
	*stack = append(*stack, q)
}

// neutralName splits a neutral body by lightness. White is a white body,
// silver is a light gray, and gray is a dark gray.
func neutralName(L float64) string {
	switch {
	case L >= 86:
		return "white"
	case L >= 64:
		return "silver"
	case L >= 22:
		return "gray"
	default:
		return "black"
	}
}

func paintName(L, a, b, chroma float64) string {
	if chroma < 12 {
		return neutralName(L)
	}
	switch {
	case b < -15:
		return "blue"
	case a < -10 && b > 0:
		return "green"
	case a > 8 && b < -8:
		return "purple"
	case b > 35 && a < 25 && L >= 55:
		return "yellow"
	case a > 8 && b > 15 && L >= 65:
		return "beige"
	case a > 40 && b > 10:
		return "red"
	case a > 12 && b > 20 && L < 45:
		return "brown"
	case a > 15 && b > 8:
		return "orange"
	default:
		return "other"
	}
}

// HasGreenBox reports whether the snapshot has Frigate's vehicle rectangle.
func HasGreenBox(img image.Image) bool {
	_, ok := strokeBox(img, markerGreen)
	return ok
}

// HasYellowBox reports whether the snapshot has Frigate's yellow person rectangle.
func HasYellowBox(img image.Image) bool {
	_, ok := strokeBox(img, markerYellow)
	return ok
}

// HasPersonBox reports whether the snapshot has a person rectangle, yellow or blue.
func HasPersonBox(img image.Image) bool {
	if HasYellowBox(img) {
		return true
	}
	_, ok := strokeBox(img, markerBlue)
	return ok
}

func markerGreen(r, g, b uint32) bool {
	rr, gg, bb := r>>8, g>>8, b>>8
	return gg > 150 && rr < 110 && bb < 110 && gg > rr+60 && gg > bb+60
}

func markerYellow(r, g, b uint32) bool {
	rr, gg, bb := int(r>>8), int(g>>8), int(b>>8)
	if rr < 160 || gg < 140 || bb > 110 {
		return false
	}
	d := rr - gg
	if d < 0 {
		d = -d
	}
	return d < 90 && rr+gg > bb+180
}

// garmentNeutral is the clothing scale. A black jacket in a snapshot often
// lands darker than a dark-gray car, so cloth below L 42 is black.
func garmentNeutral(L float64) string {
	switch {
	case L >= 86:
		return "white"
	case L >= 64:
		return "silver"
	case L >= 42:
		return "gray"
	default:
		return "black"
	}
}

// markerBlue is a person rectangle drawn blue. JPEG turns that stroke into a slate blue.
func markerBlue(r, g, b uint32) bool {
	rr, gg, bb := int(r>>8), int(g>>8), int(b>>8)
	if bb < 110 || rr > 150 || gg > 180 {
		return false
	}
	return bb > rr+28 && bb > gg+18
}

// voteGarment picks the color that covers most of the garment. Near-black cloth
// stays in the vote: dropping it left the sidewalk as the only sample. A neutral
// body is the majority pixel name, so a light sidewalk cannot average a black
// jacket up to gray.
func voteGarment(img image.Image, region image.Rectangle) Color {
	if region.Empty() {
		return Color{Name: "unknown", Quality: qualityLow}
	}
	step := 1
	if region.Dx()*region.Dy() > 20000 {
		step = 2
	}
	counts := map[string]int{}
	total := 0
	for y := region.Min.Y; y < region.Max.Y; y += step {
		for x := region.Min.X; x < region.Max.X; x += step {
			r16, g16, b16, _ := img.At(x, y).RGBA()
			if markerGreen(r16, g16, b16) || markerYellow(r16, g16, b16) {
				continue
			}
			L, a, b := rgbToLab(float64(r16)/65535, float64(g16)/65535, float64(b16)/65535)
			if L > 98 {
				continue
			}
			chroma := math.Hypot(a, b)
			name := paintName(L, a, b, chroma)
			if chroma < 12 {
				name = garmentNeutral(L)
			}
			counts[name]++
			total++
		}
	}
	if total == 0 {
		return Color{Name: "unknown", Quality: qualityLow}
	}
	best, bestN := "", 0
	for name, c := range counts {
		if name == "other" {
			continue
		}
		if c > bestN {
			best, bestN = name, c
		}
	}
	conf := float32(bestN) / float32(total)
	if best == "" || conf < 0.35 {
		return Color{Name: "unknown", Confidence: conf, Quality: qualityLow}
	}
	quality := qualityMedium
	if conf >= 0.55 {
		quality = qualityGood
	}
	return Color{Name: best, Confidence: conf, Quality: quality}
}

func rgbToLab(r, g, b float64) (L, A, B float64) {
	R, G, Bv := linear(r), linear(g), linear(b)
	X := R*0.4124564 + G*0.3575761 + Bv*0.1804375
	Y := R*0.2126729 + G*0.7151522 + Bv*0.0721750
	Z := R*0.0193339 + G*0.1191920 + Bv*0.9503041
	const xn, yn, zn = 0.95047, 1.0, 1.08883
	fy := labF(Y / yn)
	L = 116*fy - 16
	A = 500 * (labF(X/xn) - fy)
	B = 200 * (fy - labF(Z/zn))
	return L, A, B
}

func linear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func labF(t float64) float64 {
	if t > 0.008856 {
		return math.Cbrt(t)
	}
	return 7.787*t + 16.0/116.0
}
