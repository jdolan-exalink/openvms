//go:build cgo

package vehicle

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"sync"
	"time"

	ort "github.com/yalue/onnxruntime_go"
)

// BodyClassifier runs the crop model. One session is shared by the worker
// goroutines, so Classify serializes inference.
type BodyClassifier struct {
	mu      sync.Mutex
	session *ort.AdvancedSession
	input   *ort.Tensor[float32]
	output  *ort.Tensor[float32]
}

// OpenBodyClassifier loads the ONNX model. The shared library is onnxruntime
// 1.29, which matches the headers in the Go binding.
func OpenBodyClassifier(modelPath, libraryPath string) (*BodyClassifier, error) {
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("vehicle body model: %w", err)
	}
	if _, err := os.Stat(libraryPath); err != nil {
		return nil, fmt.Errorf("onnxruntime: %w", err)
	}
	ort.SetSharedLibraryPath(libraryPath)
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, err
	}
	input, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 3, bodySize, bodySize))
	if err != nil {
		return nil, err
	}
	output, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(len(bodyLabels))))
	if err != nil {
		input.Destroy()
		return nil, err
	}
	opts, err := ort.NewSessionOptions()
	if err != nil {
		input.Destroy()
		output.Destroy()
		return nil, err
	}
	defer opts.Destroy()
	if err := opts.SetIntraOpNumThreads(2); err != nil {
		input.Destroy()
		output.Destroy()
		return nil, err
	}
	if err := opts.SetInterOpNumThreads(1); err != nil {
		input.Destroy()
		output.Destroy()
		return nil, err
	}
	session, err := ort.NewAdvancedSession(modelPath,
		[]string{"image"}, []string{"logits"},
		[]ort.Value{input}, []ort.Value{output}, opts)
	if err != nil {
		input.Destroy()
		output.Destroy()
		return nil, err
	}
	return &BodyClassifier{session: session, input: input, output: output}, nil
}

// logitsForTest runs one crop and returns the eight raw logits.
func (b *BodyClassifier) logitsForTest(img image.Image) ([]float32, error) {
	if b == nil || b.session == nil || img == nil {
		return nil, fmt.Errorf("classifier is not ready")
	}
	data := prepareBody(img)
	b.mu.Lock()
	defer b.mu.Unlock()
	dst := b.input.GetData()
	if len(dst) != len(data) {
		return nil, fmt.Errorf("input length %d", len(dst))
	}
	copy(dst, data)
	if err := b.session.Run(); err != nil {
		return nil, err
	}
	src := b.output.GetData()
	out := make([]float32, len(src))
	copy(out, src)
	return out, nil
}

// Benchmark runs a few crops and returns the median milliseconds of one pass.
func (b *BodyClassifier) Benchmark() (float64, error) {
	if b == nil || b.session == nil {
		return 0, fmt.Errorf("classifier is not ready")
	}
	img := image.NewRGBA(image.Rect(0, 0, 180, 110))
	for y := 0; y < 110; y++ {
		for x := 0; x < 180; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 180, G: 180, B: 180, A: 255})
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := b.logitsForTest(img); err != nil {
			return 0, err
		}
	}
	var samples []float64
	for i := 0; i < 6; i++ {
		start := time.Now()
		if _, err := b.logitsForTest(img); err != nil {
			return 0, err
		}
		samples = append(samples, float64(time.Since(start).Microseconds())/1000)
	}
	for i := 1; i < len(samples); i++ {
		v := samples[i]
		j := i
		for j > 0 && samples[j-1] > v {
			samples[j] = samples[j-1]
			j--
		}
		samples[j] = v
	}
	return samples[len(samples)/2], nil
}

// Classify reads one vehicle crop and returns a street body type.
func (b *BodyClassifier) Classify(img image.Image) (string, float32, bool) {
	if b == nil || b.session == nil || img == nil {
		return "", 0, false
	}
	data := prepareBody(img)
	b.mu.Lock()
	defer b.mu.Unlock()
	dst := b.input.GetData()
	if len(dst) != len(data) {
		return "", 0, false
	}
	copy(dst, data)
	if err := b.session.Run(); err != nil {
		return "", 0, false
	}
	logits := b.output.GetData()
	buf := make([]float32, len(logits))
	copy(buf, logits)
	return decideBody(buf)
}
