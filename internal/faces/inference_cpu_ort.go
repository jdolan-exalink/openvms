//go:build cgo

package faces

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	faceModelInputSize  = 112
	faceModelVectorSize = 512
)

// CPUEmbeddingConfig describes the ArcFace MobileFaceNet ONNX input contract.
// The model itself must be supplied by deployment configuration; this package
// does not bundle or download model weights.
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

func (f CPUEmbeddingFactory) Open(_ context.Context) (EmbeddingRuntime, error) {
	cfg := f.Config
	if cfg.ModelPath == "" || cfg.LibraryPath == "" {
		return nil, errors.New("CPU face embedding model and ONNX Runtime library paths are required")
	}
	if cfg.InputName == "" {
		cfg.InputName = "data"
	}
	if cfg.OutputName == "" {
		cfg.OutputName = "fc1"
	}
	if _, err := os.Stat(cfg.ModelPath); err != nil {
		return nil, fmt.Errorf("face embedding model: %w", err)
	}
	if _, err := os.Stat(cfg.LibraryPath); err != nil {
		return nil, fmt.Errorf("onnxruntime: %w", err)
	}
	ort.SetSharedLibraryPath(cfg.LibraryPath)
	if !ort.IsInitialized() {
		if err := ort.InitializeEnvironment(); err != nil {
			return nil, fmt.Errorf("initialize ONNX Runtime: %w", err)
		}
	}
	input, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 3, faceModelInputSize, faceModelInputSize))
	if err != nil {
		return nil, err
	}
	output, err := ort.NewEmptyTensor[float32](ort.NewShape(1, faceModelVectorSize))
	if err != nil {
		_ = input.Destroy()
		return nil, err
	}
	opts, err := ort.NewSessionOptions()
	if err != nil {
		_ = output.Destroy()
		_ = input.Destroy()
		return nil, err
	}
	if err = opts.SetIntraOpNumThreads(2); err == nil {
		err = opts.SetInterOpNumThreads(1)
	}
	var session *ort.AdvancedSession
	if err == nil {
		session, err = ort.NewAdvancedSession(cfg.ModelPath, []string{cfg.InputName}, []string{cfg.OutputName}, []ort.Value{input}, []ort.Value{output}, opts)
	}
	cleanupErr := opts.Destroy()
	if err != nil || cleanupErr != nil {
		if session != nil {
			cleanupErr = errors.Join(cleanupErr, session.Destroy())
		}
		cleanupErr = errors.Join(cleanupErr, output.Destroy(), input.Destroy())
		return nil, errors.Join(err, cleanupErr)
	}
	return &cpuEmbeddingRuntime{session: session, input: input, output: output}, nil
}

type cpuEmbeddingRuntime struct {
	mu      sync.Mutex
	session *ort.AdvancedSession
	input   *ort.Tensor[float32]
	output  *ort.Tensor[float32]
}

func (*cpuEmbeddingRuntime) Profile() EmbeddingProfile { return EmbeddingProfileCPU }

func (r *cpuEmbeddingRuntime) Embed(ctx context.Context, img image.Image) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.session == nil || img == nil {
		return nil, errors.New("CPU face embedding runtime is not ready")
	}
	data, err := preprocessArcFace112(img)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	copy(r.input.GetData(), data)
	if err := r.session.Run(); err != nil {
		return nil, err
	}
	return normalizeEmbedding(r.output.GetData())
}

func (r *cpuEmbeddingRuntime) Smoke(ctx context.Context) error {
	img := image.NewRGBA(image.Rect(0, 0, faceModelInputSize, faceModelInputSize))
	_, err := r.Embed(ctx, img)
	return err
}

func (r *cpuEmbeddingRuntime) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var err error
	if r.session != nil {
		err = errors.Join(err, r.session.Destroy())
		r.session = nil
	}
	if r.output != nil {
		err = errors.Join(err, r.output.Destroy())
		r.output = nil
	}
	if r.input != nil {
		err = errors.Join(err, r.input.Destroy())
		r.input = nil
	}
	return err
}

// preprocessArcFace112 stretches the already detected face crop to 112x112,
// converts RGB to NCHW, and normalizes bytes to [-1, 1]. It does not align eyes;
// callers must only use this path after the model/crop protocol is validated.
func preprocessArcFace112(src image.Image) ([]float32, error) {
	if src == nil || src.Bounds().Empty() {
		return nil, errors.New("face crop is empty")
	}
	bounds := src.Bounds()
	out := make([]float32, 3*faceModelInputSize*faceModelInputSize)
	plane := faceModelInputSize * faceModelInputSize
	for y := 0; y < faceModelInputSize; y++ {
		sy := bounds.Min.Y + y*bounds.Dy()/faceModelInputSize
		for x := 0; x < faceModelInputSize; x++ {
			sx := bounds.Min.X + x*bounds.Dx()/faceModelInputSize
			r, g, b, _ := src.At(sx, sy).RGBA()
			i := y*faceModelInputSize + x
			out[i] = float32(r>>8)/127.5 - 1
			out[plane+i] = float32(g>>8)/127.5 - 1
			out[2*plane+i] = float32(b>>8)/127.5 - 1
		}
	}
	return out, nil
}

func normalizeEmbedding(values []float32) ([]float32, error) {
	if len(values) != faceModelVectorSize {
		return nil, fmt.Errorf("face embedding has %d values, want %d", len(values), faceModelVectorSize)
	}
	var norm2 float64
	for _, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, errors.New("face embedding contains a non-finite value")
		}
		norm2 += float64(value) * float64(value)
	}
	if norm2 == 0 {
		return nil, errors.New("face embedding has zero norm")
	}
	invNorm := float32(1 / math.Sqrt(norm2))
	out := make([]float32, len(values))
	for i, value := range values {
		out[i] = value * invNorm
	}
	return out, nil
}
