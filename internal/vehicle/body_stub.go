//go:build !cgo

package vehicle

import (
	"errors"
	"image"
)

// BodyClassifier is empty when the worker is built without cgo. The API binary
// does not classify images, so it keeps this stub.
type BodyClassifier struct{}

// OpenBodyClassifier reports that this binary cannot load the crop model.
func OpenBodyClassifier(modelPath, libraryPath string) (*BodyClassifier, error) {
	return nil, errors.New("vehicle body classifier needs cgo")
}

// Classify does not run a model.
func (b *BodyClassifier) Classify(img image.Image) (string, float32, bool) {
	return "", 0, false
}

// Benchmark does not run a model.
func (b *BodyClassifier) Benchmark() (float64, error) {
	return 0, errors.New("vehicle body classifier needs cgo")
}
