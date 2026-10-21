package media

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/inventory"
)

func visionCell(id, name string) *Cell { return &Cell{Vision: &VisionRef{ID: id, Name: name}} }

func TestViewInputValidatesVisionCells(t *testing.T) {
	cam := uuid.New()
	cases := map[string]struct {
		cell *Cell
		ok   bool
	}{
		"lpr channel":          {visionCell("visionai:lpr-channel", "Canal LPR"), true},
		"existing source":      {visionCell("visionai:general", "Vision AI"), true},
		"not a vision id":      {visionCell("javascript:alert(1)", "x"), false},
		"empty id":             {visionCell("", "x"), false},
		"name too long":        {visionCell("visionai:lpr-channel", strings.Repeat("a", 101)), false},
		"camera and vision":    {&Cell{CameraID: cam, Vision: &VisionRef{ID: "visionai:lpr-channel", Name: "x"}}, false},
		"plain camera kept ok": {&Cell{CameraID: cam}, true},
	}
	for name, tc := range cases {
		in := ViewInput{Name: "v", Layout: Layout{Columns: 2, Cells: []*Cell{tc.cell}}}
		err := in.validate()
		var verr *inventory.ValidationError
		if tc.ok && err != nil {
			t.Errorf("%s: unexpected error %v", name, err)
		}
		if !tc.ok && !errors.As(err, &verr) {
			t.Errorf("%s: err = %v, want a validation error", name, err)
		}
	}
}

// TestVisionCellsAreNeverRedacted keeps virtual tiles: their content is filtered per caller by the
// Vision AI, plate and event APIs, not by camera visibility.
func TestVisionCellsAreNeverRedacted(t *testing.T) {
	seen, hidden := uuid.New(), uuid.New()
	v := View{Layout: Layout{Columns: 3, Cells: []*Cell{{CameraID: seen}, {CameraID: hidden}, visionCell("visionai:faces-channel", "Canal Rostros")}}}
	visible := map[uuid.UUID]bool{seen: true}

	redact(&v, visible)

	if v.Layout.Cells[0] == nil || v.Layout.Cells[1] != nil {
		t.Fatalf("camera cells: %+v / %+v, want the visible one kept and the hidden one emptied", v.Layout.Cells[0], v.Layout.Cells[1])
	}
	if c := v.Layout.Cells[2]; c == nil || c.Vision == nil || c.Vision.ID != "visionai:faces-channel" {
		t.Fatalf("vision cell was redacted: %+v", c)
	}
	if visionCell("visionai:lpr-channel", "x").hiddenFrom(visible) {
		t.Fatal("a vision cell must never count as hidden")
	}
}
