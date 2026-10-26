package frigate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
)

// ErrLabelsUnknown means the object labels of the detector model cannot be determined, for
// example because a custom labelmap file lives on the Frigate host.
var ErrLabelsUnknown = errors.New("the model labels of this frigate are unknown")

// cocoLabels is Frigate's default labelmap (COCO, 91 ids with gaps), by index. "???" marks ids
// the model never reports.
var cocoLabels = []string{
	"person", "bicycle", "car", "motorcycle", "airplane", "bus", "train", "truck", "boat",
	"traffic light", "fire hydrant", "???", "stop sign", "parking meter", "bench", "bird", "cat",
	"dog", "horse", "sheep", "cow", "elephant", "bear", "zebra", "giraffe", "???", "backpack",
	"umbrella", "???", "???", "handbag", "tie", "suitcase", "frisbee", "skis", "snowboard",
	"sports ball", "kite", "baseball bat", "baseball glove", "skateboard", "surfboard",
	"tennis racket", "bottle", "???", "wine glass", "cup", "fork", "knife", "spoon", "bowl",
	"banana", "apple", "sandwich", "orange", "broccoli", "carrot", "hot dog", "pizza", "donut",
	"cake", "chair", "couch", "potted plant", "bed", "???", "dining table", "???", "???", "toilet",
	"???", "tv", "laptop", "mouse", "remote", "keyboard", "cell phone", "microwave", "oven",
	"toaster", "sink", "refrigerator", "???", "book", "clock", "vase", "scissors", "teddy bear",
	"hair drier", "toothbrush",
}

// ModelLabels returns the sorted object labels the detector model reports, read from the
// model section of GET /api/config: Frigate's default COCO labelmap with the inline
// labelmap overrides applied. A labelmap_path points to a file on the Frigate host, which the
// API cannot read, so it yields ErrLabelsUnknown.
func (a *v017) ModelLabels(ctx context.Context) ([]string, error) {
	var full struct {
		Model struct {
			Labelmap     map[string]string `json:"labelmap"`
			LabelmapPath *string           `json:"labelmap_path"`
		} `json:"model"`
	}
	if err := a.c.getJSON(ctx, "/api/config", nil, &full); err != nil {
		return nil, err
	}
	if full.Model.LabelmapPath != nil && *full.Model.LabelmapPath != "" {
		return nil, ErrLabelsUnknown
	}
	byIndex := map[int]string{}
	for i, l := range cocoLabels {
		byIndex[i] = l
	}
	for k, l := range full.Model.Labelmap {
		i, err := strconv.Atoi(k)
		if err != nil || i < 0 {
			return nil, fmt.Errorf("%w: labelmap key %q", ErrLabelsUnknown, k)
		}
		byIndex[i] = l
	}
	var out []string
	for _, l := range byIndex {
		if l != "" && l != "???" {
			out = append(out, l)
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}
