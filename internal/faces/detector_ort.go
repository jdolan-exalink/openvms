//go:build cgo

package faces

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// YuNetConfig describes the OpenCV Zoo YuNet face detector. The model is supplied by deployment
// configuration; this package does not bundle or download weights.
type YuNetConfig struct {
	ModelPath   string
	LibraryPath string
	// MinScore overrides DefaultFaceDetectionScore when positive.
	MinScore float64
}

type yunetDetector struct {
	mu       sync.Mutex
	minScore float64
	session  *ort.AdvancedSession
	input    *ort.Tensor[float32]
	cls, obj [3]*ort.Tensor[float32]
}

// OpenYuNet loads the detector and proves it runs once before returning.
func OpenYuNet(cfg YuNetConfig) (FaceDetector, error) {
	if cfg.ModelPath == "" || cfg.LibraryPath == "" {
		return nil, errors.New("face detector model and ONNX Runtime library paths are required")
	}
	if _, err := os.Stat(cfg.ModelPath); err != nil {
		return nil, fmt.Errorf("face detector model: %w", err)
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
	d := &yunetDetector{minScore: cfg.MinScore}
	if d.minScore <= 0 {
		d.minScore = DefaultFaceDetectionScore
	}
	if err := d.open(cfg.ModelPath); err != nil {
		_ = d.Close()
		return nil, err
	}
	if _, err := d.HasFace(context.Background(), image.NewRGBA(image.Rect(0, 0, 32, 32))); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("face detector smoke inference: %w", err)
	}
	return d, nil
}

func (d *yunetDetector) open(modelPath string) error {
	var err error
	if d.input, err = ort.NewEmptyTensor[float32](ort.NewShape(1, 3, yunetInputSize, yunetInputSize)); err != nil {
		return err
	}
	names := []string{}
	values := []ort.Value{}
	for s, stride := range yunetStrides {
		shape := ort.NewShape(1, int64(yunetAnchors(stride)), 1)
		if d.cls[s], err = ort.NewEmptyTensor[float32](shape); err != nil {
			return err
		}
		if d.obj[s], err = ort.NewEmptyTensor[float32](shape); err != nil {
			return err
		}
		names = append(names, fmt.Sprintf("cls_%d", stride), fmt.Sprintf("obj_%d", stride))
		values = append(values, d.cls[s], d.obj[s])
	}
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return err
	}
	if err = opts.SetIntraOpNumThreads(2); err == nil {
		err = opts.SetInterOpNumThreads(1)
	}
	if err == nil {
		d.session, err = ort.NewAdvancedSession(modelPath, []string{"input"}, names, []ort.Value{d.input}, values, opts)
	}
	return errors.Join(err, opts.Destroy())
}

func (d *yunetDetector) HasFace(ctx context.Context, img image.Image) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	data, err := preprocessYuNet(img)
	if err != nil {
		return false, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.session == nil {
		return false, errors.New("face detector is not ready")
	}
	copy(d.input.GetData(), data)
	if err := d.session.Run(); err != nil {
		return false, err
	}
	var cls, obj [3][]float32
	for s := range yunetStrides {
		cls[s], obj[s] = d.cls[s].GetData(), d.obj[s].GetData()
	}
	score, err := bestYuNetScore(cls, obj)
	return score >= d.minScore, err
}

func (d *yunetDetector) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var err error
	if d.session != nil {
		err = errors.Join(err, d.session.Destroy())
		d.session = nil
	}
	for s := range yunetStrides {
		for _, t := range []*ort.Tensor[float32]{d.cls[s], d.obj[s]} {
			if t != nil {
				err = errors.Join(err, t.Destroy())
			}
		}
		d.cls[s], d.obj[s] = nil, nil
	}
	if d.input != nil {
		err = errors.Join(err, d.input.Destroy())
		d.input = nil
	}
	return err
}
