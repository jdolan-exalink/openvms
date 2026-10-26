package inventory

import (
	"reflect"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/frigate"
)

func TestHealthProblemsNamesEveryReasonAServerIsDegraded(t *testing.T) {
	full := &frigate.Storage{TotalMB: 1000, UsedMB: 950}
	roomy := &frigate.Storage{TotalMB: 1000, UsedMB: 360}
	tests := []struct {
		name    string
		offline []string
		rec     *frigate.Storage
		stalled bool
		want    []HealthProblem
	}{
		{name: "healthy server has no problems", rec: roomy},
		{name: "offline cameras are listed", offline: []string{"Escuela422", "Banco"}, rec: roomy,
			want: []HealthProblem{{Code: ProblemCamerasOffline, Cameras: []string{"Escuela422", "Banco"}}}},
		{name: "storage over the threshold reports the used percentage", rec: full,
			want: []HealthProblem{{Code: ProblemStorageHigh, UsedPercent: 95}}},
		{name: "a stalled detection is a problem", rec: roomy, stalled: true,
			want: []HealthProblem{{Code: ProblemDetectionStalled}}},
		{name: "several reasons keep a stable order", offline: []string{"A"}, rec: full, stalled: true,
			want: []HealthProblem{{Code: ProblemCamerasOffline, Cameras: []string{"A"}}, {Code: ProblemStorageHigh, UsedPercent: 95}, {Code: ProblemDetectionStalled}}},
		{name: "unknown storage is not a problem", rec: nil},
		{name: "storage exactly at the threshold is not a problem", rec: &frigate.Storage{TotalMB: 1000, UsedMB: 900}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := healthProblems(tt.offline, tt.rec, tt.stalled)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("problems = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// Frigate's mosaic channel is not a camera: it never reports stats, so it must not make a server degraded.
func TestBirdseyeIsNotACameraThatCanBeOffline(t *testing.T) {
	for _, name := range []string{"birdseye", "BirdsEye", "BIRDSEYE"} {
		if !isVirtualCamera(name) {
			t.Errorf("%q should be a virtual camera", name)
		}
	}
	for _, name := range []string{"Escuela422", "birdseye2", ""} {
		if isVirtualCamera(name) {
			t.Errorf("%q should be a real camera", name)
		}
	}
}
