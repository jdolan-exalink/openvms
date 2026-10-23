//go:build cgo && hailo

package faces

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"sync"

	hailort "github.com/zhooravell/go-hailort"
)

// Hailo8EmbeddingConfig points to a Hailo-8 compatible HEF. The HEF must
// accept 112x112 RGB uint8 pixels and expose a single 512-value embedding.
type Hailo8EmbeddingConfig struct {
	HEFPath    string
	OutputName string
}

type Hailo8EmbeddingFactory struct {
	Config Hailo8EmbeddingConfig
}

func (Hailo8EmbeddingFactory) Profile() EmbeddingProfile { return EmbeddingProfileHailo8 }

func (f Hailo8EmbeddingFactory) Open(ctx context.Context) (EmbeddingRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.Config.HEFPath == "" {
		return nil, errors.New("Hailo-8 face embedding HEF path is required")
	}
	if _, err := os.Stat(f.Config.HEFPath); err != nil {
		return nil, fmt.Errorf("Hailo-8 face embedding HEF: %w", err)
	}
	device, err := hailort.Open(f.Config.HEFPath, hailort.WithOutputFormat(hailort.FormatTypeFloat32))
	if err != nil {
		return nil, fmt.Errorf("open Hailo-8 face embedding HEF: %w", err)
	}
	runtime, err := newHailo8EmbeddingRuntime(device, f.Config.OutputName)
	if err != nil {
		device.Close()
		return nil, err
	}
	return runtime, nil
}

type hailo8Device interface {
	InFrameSizes() []int
	OutputNames() []string
	Infer(context.Context, []byte) ([]hailort.Tensor, error)
	Close()
}

type hailo8EmbeddingRuntime struct {
	mu         sync.Mutex
	device     hailo8Device
	outputName string
}

func newHailo8EmbeddingRuntime(device hailo8Device, outputName string) (*hailo8EmbeddingRuntime, error) {
	if device == nil {
		return nil, errors.New("Hailo-8 device is not ready")
	}
	name, err := validateHailoContract(device.InFrameSizes(), device.OutputNames(), outputName)
	if err != nil {
		return nil, err
	}
	return &hailo8EmbeddingRuntime{device: device, outputName: name}, nil
}

func (*hailo8EmbeddingRuntime) Profile() EmbeddingProfile { return EmbeddingProfileHailo8 }

func (r *hailo8EmbeddingRuntime) Embed(ctx context.Context, img image.Image) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || img == nil {
		return nil, errors.New("Hailo-8 face embedding runtime is not ready")
	}
	input, err := preprocessHailoRGB112(img)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.device == nil {
		return nil, errors.New("Hailo-8 face embedding runtime is not ready")
	}
	outputs, err := r.device.Infer(ctx, input)
	if err != nil {
		return nil, err
	}
	for _, output := range outputs {
		if output.Name == r.outputName {
			return normalizeEmbedding(output.Data)
		}
	}
	return nil, fmt.Errorf("Hailo-8 HEF did not return output %q", r.outputName)
}

func (r *hailo8EmbeddingRuntime) Smoke(ctx context.Context) error {
	img := image.NewRGBA(image.Rect(0, 0, faceModelInputSize, faceModelInputSize))
	_, err := r.Embed(ctx, img)
	return err
}

func (r *hailo8EmbeddingRuntime) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.device != nil {
		r.device.Close()
		r.device = nil
	}
	return nil
}

func validateHailoContract(inputSizes []int, outputNames []string, requestedOutput string) (string, error) {
	if len(inputSizes) != 1 || inputSizes[0] != faceModelInputSize*faceModelInputSize*3 {
		return "", fmt.Errorf("Hailo-8 HEF must have one 112x112 RGB input (%d bytes); got %v", faceModelInputSize*faceModelInputSize*3, inputSizes)
	}
	if len(outputNames) != 1 {
		return "", fmt.Errorf("Hailo-8 HEF must have one embedding output; got %d outputs", len(outputNames))
	}
	if requestedOutput != "" && requestedOutput != outputNames[0] {
		return "", fmt.Errorf("Hailo-8 HEF has output %q, requested %q", outputNames[0], requestedOutput)
	}
	return outputNames[0], nil
}
