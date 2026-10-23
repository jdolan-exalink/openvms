package faces

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestSelectEmbeddingRuntimePrefersUsableHardwareProfile(t *testing.T) {
	var tried []EmbeddingProfile
	selection, err := SelectEmbeddingRuntime(context.Background(), []EmbeddingRuntimeFactory{
		fakeEmbeddingFactory{profile: EmbeddingProfileCPU, tried: &tried},
		fakeEmbeddingFactory{profile: EmbeddingProfileHailo8, tried: &tried},
		fakeEmbeddingFactory{profile: EmbeddingProfileNVIDIA, tried: &tried},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Profile != EmbeddingProfileNVIDIA || selection.Runtime == nil {
		t.Fatalf("selection = %+v, want usable NVIDIA runtime", selection)
	}
	if !reflect.DeepEqual(tried, []EmbeddingProfile{EmbeddingProfileNVIDIA}) {
		t.Fatalf("attempt order = %v, want NVIDIA first and stop on success", tried)
	}
}

func TestSelectEmbeddingRuntimeFallsBackAfterProviderAndSmokeFailures(t *testing.T) {
	var tried []EmbeddingProfile
	nvidia := &fakeEmbeddingRuntime{profile: EmbeddingProfileNVIDIA, smokeErr: errors.New("CUDA provider unavailable")}
	hailo := &fakeEmbeddingRuntime{profile: EmbeddingProfileHailo8, smokeErr: errors.New("HEF smoke inference failed")}
	cpu := &fakeEmbeddingRuntime{profile: EmbeddingProfileCPU}
	selection, err := SelectEmbeddingRuntime(context.Background(), []EmbeddingRuntimeFactory{
		fakeEmbeddingFactory{profile: EmbeddingProfileCPU, runtime: cpu, tried: &tried},
		fakeEmbeddingFactory{profile: EmbeddingProfileHailo8, runtime: hailo, tried: &tried},
		fakeEmbeddingFactory{profile: EmbeddingProfileNVIDIA, runtime: nvidia, tried: &tried},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Profile != EmbeddingProfileCPU || selection.Runtime != cpu {
		t.Fatalf("selection = %+v, want CPU fallback", selection)
	}
	if !reflect.DeepEqual(tried, []EmbeddingProfile{EmbeddingProfileNVIDIA, EmbeddingProfileHailo8, EmbeddingProfileCPU}) {
		t.Fatalf("attempt order = %v", tried)
	}
	if !nvidia.closed || !hailo.closed {
		t.Fatalf("failed runtime cleanup: NVIDIA closed=%v, Hailo closed=%v", nvidia.closed, hailo.closed)
	}
}

func TestSelectEmbeddingRuntimeFallsBackAfterProviderInitializationFailure(t *testing.T) {
	var tried []EmbeddingProfile
	hailo := &fakeEmbeddingRuntime{profile: EmbeddingProfileHailo8}
	selection, err := SelectEmbeddingRuntime(context.Background(), []EmbeddingRuntimeFactory{
		fakeEmbeddingFactory{profile: EmbeddingProfileNVIDIA, openErr: errors.New("CUDA libraries unavailable"), tried: &tried},
		fakeEmbeddingFactory{profile: EmbeddingProfileHailo8, runtime: hailo, tried: &tried},
		fakeEmbeddingFactory{profile: EmbeddingProfileCPU, tried: &tried},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Profile != EmbeddingProfileHailo8 || selection.Runtime != hailo {
		t.Fatalf("selection = %+v, want Hailo-8 after NVIDIA initialization failure", selection)
	}
	if !reflect.DeepEqual(tried, []EmbeddingProfile{EmbeddingProfileNVIDIA, EmbeddingProfileHailo8}) {
		t.Fatalf("attempt order = %v", tried)
	}
}

func TestSelectEmbeddingRuntimeReportsUnavailableWhenAllProfilesFail(t *testing.T) {
	var tried []EmbeddingProfile
	_, err := SelectEmbeddingRuntime(context.Background(), []EmbeddingRuntimeFactory{
		fakeEmbeddingFactory{profile: EmbeddingProfileCPU, openErr: errors.New("CPU model missing"), tried: &tried},
	})
	if err == nil {
		t.Fatal("expected unavailable error")
	}
	if !reflect.DeepEqual(tried, []EmbeddingProfile{EmbeddingProfileCPU}) {
		t.Fatalf("attempt order = %v, want CPU attempt", tried)
	}
}

type fakeEmbeddingFactory struct {
	profile EmbeddingProfile
	runtime *fakeEmbeddingRuntime
	openErr error
	tried   *[]EmbeddingProfile
}

func (f fakeEmbeddingFactory) Profile() EmbeddingProfile { return f.profile }

func (f fakeEmbeddingFactory) Open(context.Context) (EmbeddingRuntime, error) {
	*f.tried = append(*f.tried, f.profile)
	if f.runtime == nil {
		if f.openErr != nil {
			return nil, f.openErr
		}
		return &fakeEmbeddingRuntime{profile: f.profile}, nil
	}
	return f.runtime, f.openErr
}

type fakeEmbeddingRuntime struct {
	profile  EmbeddingProfile
	smokeErr error
	closed   bool
}

func (r *fakeEmbeddingRuntime) Profile() EmbeddingProfile { return r.profile }

func (r *fakeEmbeddingRuntime) Smoke(context.Context) error { return r.smokeErr }

func (r *fakeEmbeddingRuntime) Close() error {
	r.closed = true
	return nil
}
