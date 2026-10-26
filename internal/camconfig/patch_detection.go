package camconfig

import (
	"context"
	"fmt"
	"slices"

	"github.com/jdolan-exalink/openvms/internal/camconfig/catalog"
)

// ModelLabelSource is implemented by engines that can say which object labels their model
// supports. It is optional: engines that cannot report them leave the labels "unknown".
type ModelLabelSource interface {
	ModelLabels(ctx context.Context) ([]string, error)
}

// ModelLabels returns the labels the engine's model supports, or nil when the engine cannot
// say (nil means unknown, an empty non-nil slice means "supports nothing").
func ModelLabels(ctx context.Context, engine any) []string {
	src, ok := engine.(ModelLabelSource)
	if !ok {
		return nil
	}
	labels, err := src.ModelLabels(ctx)
	if err != nil || len(labels) == 0 {
		return nil
	}
	out := append([]string{}, labels...)
	slices.Sort(out)
	return slices.Compact(out)
}

// validateSelection checks detection.selection: it excludes detection.objects and every id
// must be a catalog object or category.
func validateSelection(d *DetectionPatch) error {
	if d == nil || d.Selection == nil {
		return nil
	}
	if d.Objects != nil {
		return invalid("detection: send either selection or objects, not both")
	}
	cat := catalog.Default()
	for _, id := range *d.Selection {
		if _, ok := cat.Object(id); ok || cat.HasCategory(id) {
			continue
		}
		return invalid("detection.selection: %q is not a catalog object or category", id)
	}
	return nil
}

// ResolveSelection turns detection.selection into the engine labels of detection.objects
// using the labels the model supports (nil = unknown). The second result describes the ids
// the model cannot honor; they are never dropped silently, callers must report them.
func ResolveSelection(p Patch, available []string) (Patch, []string) {
	if p.Detection == nil || p.Detection.Selection == nil {
		return p, nil
	}
	cat := catalog.Default()
	labels, unsupported := cat.ExpandForModel(catalog.EngineFrigate, *p.Detection.Selection, available)
	d := *p.Detection
	d.Selection = nil
	d.Objects = &labels
	p.Detection = &d
	out := make([]string, 0, len(unsupported))
	for _, id := range unsupported {
		name := id
		if o, ok := cat.Object(id); ok {
			name = fmt.Sprintf("%s (%s)", o.Label, id)
		}
		out = append(out, fmt.Sprintf("detection.selection: %s no puede ser distinguido por el modelo del motor", name))
	}
	return p, out
}

// selectionChanges lists, in Spanish catalog labels, what moving the tracked labels from cur
// to want turns on or off: one line per category that flips completely and one per object.
// Raw labels the catalog does not know are covered by the plain detection.objects change.
func selectionChanges(cur, want []string) []Change {
	cat := catalog.Default()
	from := cat.CollapseFromEngine(catalog.EngineFrigate, cur)
	to := cat.CollapseFromEngine(catalog.EngineFrigate, want)
	var out []Change
	for _, c := range cat.Categories {
		a, b := slices.Contains(from.Categories, c.ID), slices.Contains(to.Categories, c.ID)
		if a != b {
			out = append(out, Change{Path: "detection.selection." + c.ID, Label: c.Label, From: a, To: b})
		}
		for _, o := range c.Objects {
			a, b := slices.Contains(from.Objects, o.ID), slices.Contains(to.Objects, o.ID)
			if a != b {
				out = append(out, Change{Path: "detection.selection." + o.ID, Label: o.Label, From: a, To: b})
			}
		}
	}
	return out
}

// detectionView fills the catalog view of the tracked labels.
func detectionView(d *Detection) {
	sel := catalog.Default().CollapseFromEngine(catalog.EngineFrigate, d.Objects)
	d.Selection, d.Categories, d.Raw = sel.Objects, sel.Categories, sel.Raw
}
