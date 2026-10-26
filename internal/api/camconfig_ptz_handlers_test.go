package api

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

func TestPtzTourContractDeclaresPermissionAndConflictResponses(t *testing.T) {
	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	collection := spec.Paths.Find("/api/v1/cameras/{cameraId}/ptz/tours")
	item := spec.Paths.Find("/api/v1/cameras/{cameraId}/ptz/tours/{tourId}")
	if collection == nil || collection.Get == nil || collection.Post == nil || item == nil || item.Put == nil || item.Delete == nil {
		t.Fatal("PTZ tour operations are missing from the contract")
	}
	for _, c := range []struct {
		name string
		has  func(int) bool
		want []int
	}{
		{"list", func(s int) bool { return collection.Get.Responses.Status(s) != nil }, []int{401, 403, 404}},
		{"create", func(s int) bool { return collection.Post.Responses.Status(s) != nil }, []int{400, 403, 404, 409}},
		{"update", func(s int) bool { return item.Put.Responses.Status(s) != nil }, []int{400, 403, 404, 409}},
		{"delete", func(s int) bool { return item.Delete.Responses.Status(s) != nil }, []int{403, 404}},
	} {
		for _, s := range c.want {
			if !c.has(s) {
				t.Errorf("%s must declare %d", c.name, s)
			}
		}
	}
}

func TestPtzTourMappingRoundTrips(t *testing.T) {
	name := "Gate"
	repeat := 4
	in := gen.CameraPtzTourInput{Name: "Round", RepeatCount: &repeat, Steps: []gen.CameraPtzTourStep{
		{PresetToken: "1", PresetName: &name, DwellSeconds: 10, Speed: 0.5},
		{PresetToken: "2", DwellSeconds: 0, Speed: 1},
	}}
	got := cameraPtzTourInput(in)
	if got.Name != "Round" || got.RepeatCount != 4 || len(got.Steps) != 2 || got.Steps[0].PresetName != "Gate" || got.Steps[1].PresetName != "" {
		t.Fatalf("input = %+v", got)
	}
	now := time.Now()
	out := toCameraPtzTour(inventory.PTZTour{ID: uuid.New(), CameraID: uuid.New(), Name: "Round", Steps: got.Steps, RepeatCount: 4, CreatedAt: now, UpdatedAt: now})
	if out.Steps[0].PresetName == nil || *out.Steps[0].PresetName != "Gate" || out.Steps[1].PresetName != nil || out.RepeatCount != 4 {
		t.Fatalf("output = %+v", out)
	}
}
