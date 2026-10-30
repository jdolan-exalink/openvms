package events

import (
	"bytes"
	"image"
	"image/jpeg"
)

// CropLicensePlate extracts the license plate sub-image from a full-frame JPEG based on normalized
// box coordinates [top, left, height, width] (0.0 to 1.0) with margin for readability.
// If the box is invalid or cropping fails, the original jpeg bytes are returned unchanged.
func CropLicensePlate(jpegBytes []byte, box []float64) ([]byte, error) {
	if len(box) != 4 {
		return jpegBytes, nil
	}
	top, left, height, width := box[0], box[1], box[2], box[3]
	if height <= 0 || width <= 0 || top < 0 || left < 0 || top >= 1.0 || left >= 1.0 {
		return jpegBytes, nil
	}

	img, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return jpegBytes, nil
	}

	ymin := int(top * float64(h))
	xmin := int(left * float64(w))
	boxH := int(height * float64(h))
	boxW := int(width * float64(w))
	if boxH <= 0 || boxW <= 0 {
		return jpegBytes, nil
	}

	// 20% margin around the plate
	padY := int(float64(boxH) * 0.20)
	padX := int(float64(boxW) * 0.20)
	x0 := max(0, xmin-padX)
	y0 := max(0, ymin-padY)
	x1 := min(w, xmin+boxW+padX)
	y1 := min(h, ymin+boxH+padY)
	if x1 <= x0 || y1 <= y0 {
		return jpegBytes, nil
	}

	rect := image.Rect(x0, y0, x1, y1)
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	si, ok := img.(subImager)
	if !ok {
		return jpegBytes, nil
	}

	sub := si.SubImage(rect)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, sub, &jpeg.Options{Quality: 92}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
