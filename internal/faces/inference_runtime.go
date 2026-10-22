package faces

import (
	"context"
	"errors"
	"fmt"
)

type EmbeddingProfile string

const (
	EmbeddingProfileNVIDIA EmbeddingProfile = "nvidia"
	EmbeddingProfileHailo8 EmbeddingProfile = "hailo8"
	EmbeddingProfileCPU    EmbeddingProfile = "cpu"
)

var ErrEmbeddingRuntimeUnavailable = errors.New("face embedding runtime unavailable")

// EmbeddingRuntime must smoke-infer a synthetic input before it is selected.
// Implementations must not use a retained face crop for readiness checks.
type EmbeddingRuntime interface {
	Profile() EmbeddingProfile
	Smoke(context.Context) error
	Close() error
}

// EmbeddingRuntimeFactory opens a runtime only when its provider and model
// artifact are available to the central worker. Open must clean up any partial
// initialization when it returns an error.
type EmbeddingRuntimeFactory interface {
	Profile() EmbeddingProfile
	Open(context.Context) (EmbeddingRuntime, error)
}

type EmbeddingRuntimeSelection struct {
	Profile EmbeddingProfile
	Runtime EmbeddingRuntime
}

// SelectEmbeddingRuntime prefers NVIDIA, then Hailo-8, then CPU. Device
// visibility alone is not sufficient: the factory must initialize the real
// provider and Smoke must successfully execute an inference before selection.
func SelectEmbeddingRuntime(ctx context.Context, factories []EmbeddingRuntimeFactory) (EmbeddingRuntimeSelection, error) {
	factoryByProfile := make(map[EmbeddingProfile]EmbeddingRuntimeFactory, len(factories))
	for _, factory := range factories {
		if factory == nil {
			continue
		}
		profile := factory.Profile()
		if _, exists := factoryByProfile[profile]; !exists {
			factoryByProfile[profile] = factory
		}
	}

	var failures []error
	for _, profile := range []EmbeddingProfile{EmbeddingProfileNVIDIA, EmbeddingProfileHailo8, EmbeddingProfileCPU} {
		factory, exists := factoryByProfile[profile]
		if !exists {
			continue
		}
		if err := ctx.Err(); err != nil {
			return EmbeddingRuntimeSelection{}, err
		}

		runtime, err := factory.Open(ctx)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s provider initialization: %w", profile, err))
			continue
		}
		if runtime == nil {
			failures = append(failures, fmt.Errorf("%s provider initialization returned no runtime", profile))
			continue
		}
		if runtime.Profile() != profile {
			closeErr := runtime.Close()
			failures = append(failures, errors.Join(
				fmt.Errorf("%s factory returned %s runtime", profile, runtime.Profile()), closeErr,
			))
			continue
		}
		if err := runtime.Smoke(ctx); err != nil {
			closeErr := runtime.Close()
			failures = append(failures, errors.Join(fmt.Errorf("%s smoke inference: %w", profile, err), closeErr))
			continue
		}
		return EmbeddingRuntimeSelection{Profile: profile, Runtime: runtime}, nil
	}

	return EmbeddingRuntimeSelection{}, errors.Join(append([]error{ErrEmbeddingRuntimeUnavailable}, failures...)...)
}
