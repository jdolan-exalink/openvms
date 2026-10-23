//go:build !cgo

package faces

import (
	"context"
	"errors"
	"image"
)

type CPUEmbeddingConfig struct {
	ModelPath   string
	LibraryPath string
	InputName   string
	OutputName  string
}

type CPUEmbeddingFactory struct {
	Config CPUEmbeddingConfig
}

func (CPUEmbeddingFactory) Profile() EmbeddingProfile { return EmbeddingProfileCPU }

func (CPUEmbeddingFactory) Open(context.Context) (EmbeddingRuntime, error) {
	return nil, errors.New("CPU face embedding runtime needs cgo")
}

func preprocessArcFace112(image.Image) ([]float32, error) {
	return nil, errors.New("CPU face embedding runtime needs cgo")
}

func normalizeEmbedding([]float32) ([]float32, error) {
	return nil, errors.New("CPU face embedding runtime needs cgo")
}
