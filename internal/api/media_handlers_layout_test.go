package api

import (
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

func TestViewLayoutRoundTripsVisionCells(t *testing.T) {
	cam := uuid.New()
	q := gen.ViewCellQuality("main")
	in := gen.ViewLayout{Columns: 2, Cells: []gen.ViewCell{
		{CameraId: &cam, Quality: &q},
		{Vision: &gen.ViewVisionRef{Id: "visionai:lpr-channel", Name: "Canal LPR"}},
		{},
	}}

	out := toLayout(fromLayout(in))

	if len(out.Cells) != 3 || out.Cells[0].CameraId == nil || *out.Cells[0].CameraId != cam {
		t.Fatalf("camera cell lost: %+v", out.Cells)
	}
	if v := out.Cells[1].Vision; v == nil || v.Id != "visionai:lpr-channel" || v.Name != "Canal LPR" || out.Cells[1].CameraId != nil {
		t.Fatalf("vision cell = %+v, want the LPR channel without a camera", out.Cells[1])
	}
	if out.Cells[2].CameraId != nil || out.Cells[2].Vision != nil {
		t.Fatalf("empty cell must stay empty: %+v", out.Cells[2])
	}
}
