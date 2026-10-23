//go:build !cgo || !hailo

package faces

import (
	"context"
	"errors"
)

type Hailo8EmbeddingConfig struct {
	HEFPath    string
	OutputName string
}

type Hailo8EmbeddingFactory struct {
	Config Hailo8EmbeddingConfig
}

func (Hailo8EmbeddingFactory) Profile() EmbeddingProfile { return EmbeddingProfileHailo8 }

func (Hailo8EmbeddingFactory) Open(context.Context) (EmbeddingRuntime, error) {
	return nil, errors.New("Hailo-8 face embedding provider requires a cgo build with the hailo tag and HailoRT installed")
}
