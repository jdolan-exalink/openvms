package watermark

import (
	"bytes"
	"fmt"
	stdimg "image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// baseFont is parsed once: it is compiled into the binary (gofont/goregular), so parsing it
// can never fail at runtime — a parse error here would be a programming error, not a
// caller's, hence the panic in init rather than a returned error on every call.
var baseFont *opentype.Font

func init() {
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		panic(fmt.Sprintf("watermark: parse embedded font: %v", err))
	}
	baseFont = f
}

func faceForHeight(px int) (font.Face, error) {
	return opentype.NewFace(baseFont, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingFull})
}

// BurnPhoto draws a semi-transparent bar across the bottom of the JPEG in data, with text
// (and logo, if not nil) on it, and returns a new JPEG at the same dimensions. It never
// mutates or retains data.
func BurnPhoto(data []byte, text string, logo stdimg.Image) ([]byte, error) {
	src, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode photo: %w", err)
	}
	b := src.Bounds()
	dst := stdimg.NewRGBA(b)
	stddraw.Draw(dst, b, src, b.Min, stddraw.Src)

	barHeight := b.Dy() / 14
	if barHeight < 24 {
		barHeight = 24
	}
	if barHeight > 64 {
		barHeight = 64
	}
	barRect := stdimg.Rect(b.Min.X, b.Max.Y-barHeight, b.Max.X, b.Max.Y)
	stddraw.Draw(dst, barRect, stdimg.NewUniform(color.NRGBA{R: 0, G: 0, B: 0, A: 170}), stdimg.Point{}, stddraw.Over)

	x := b.Min.X + barHeight/4
	if logo != nil {
		logoSize := barHeight - 8
		logoRect := stdimg.Rect(x, barRect.Min.Y+4, x+logoSize, barRect.Min.Y+4+logoSize)
		draw.CatmullRom.Scale(dst, logoRect, logo, logo.Bounds(), stddraw.Over, nil)
		x = logoRect.Max.X + barHeight/4
	}

	face, err := faceForHeight(barHeight * 55 / 100)
	if err != nil {
		return nil, fmt.Errorf("build font face: %w", err)
	}
	defer face.Close()

	d := &font.Drawer{Dst: dst, Src: stdimg.White, Face: face, Dot: fixed.P(x, barRect.Min.Y+barHeight*7/10)}
	d.DrawString(text)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 95}); err != nil {
		return nil, fmt.Errorf("encode photo: %w", err)
	}
	return out.Bytes(), nil
}
