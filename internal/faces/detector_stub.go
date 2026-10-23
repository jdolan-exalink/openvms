//go:build !cgo

package faces

import "errors"

// YuNetConfig must mirror the fields of the cgo definition in detector_ort.go.
type YuNetConfig struct {
	ModelPath   string
	LibraryPath string
	MinScore    float64
}

func OpenYuNet(YuNetConfig) (FaceDetector, error) {
	return nil, errors.New("face detector needs cgo")
}
