// Package catalog is the normalized OpenVMS object catalog.
//
// The UI never shows an engine's raw label list. It shows Categories made of
// Objects, and each Object maps to zero or more labels per engine kind
// ("frigate", later "provision", ...). The catalog translates both ways and
// never loses a label it does not know: those travel as Selection.Raw.
//
// The package has no dependency on Frigate packages or the API.
package catalog

import (
	"fmt"
	"slices"
	"sort"
)

// Engine kinds with a mapping in the default catalog.
const EngineFrigate = "frigate"

// Object is one selectable OpenVMS object.
type Object struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Engine maps an engine kind to the labels that engine uses for this
	// object. A missing or empty entry means the engine cannot report it:
	// the capability is recorded as absent, never approximated.
	Engine map[string][]string `json:"engine"`
	// RequiresModel marks objects that exist only if the engine model was
	// trained for them (for example PPE).
	RequiresModel bool `json:"requiresModel,omitempty"`
}

// Category groups objects for the UI. Its ID is also a valid selection id
// meaning "every member".
type Category struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Objects []Object `json:"objects"`
}

// Selection is the normalized form of a set of engine labels.
type Selection struct {
	// Objects are OpenVMS object ids, in catalog order.
	Objects []string `json:"objects"`
	// Categories are the ids of categories whose every engine-supported
	// member is selected (the "activate all" toggle state).
	Categories []string `json:"categories"`
	// Raw are labels the catalog does not know, sorted and deduplicated.
	Raw []string `json:"raw"`
}

// Catalog is an ordered, indexed set of categories.
type Catalog struct {
	Categories []Category `json:"categories"`

	objects map[string]objRef
	cats    map[string]int
	labels  map[string]map[string]string // engine -> label -> object id
}

type objRef struct{ cat, obj int }

// New validates and indexes categories. Ids must be unique and an engine label
// may belong to a single object.
func New(categories []Category) (Catalog, error) {
	c := Catalog{
		Categories: categories,
		objects:    map[string]objRef{},
		cats:       map[string]int{},
		labels:     map[string]map[string]string{},
	}
	for ci, cat := range categories {
		if _, dup := c.cats[cat.ID]; dup {
			return Catalog{}, fmt.Errorf("catalog: duplicate category id %q", cat.ID)
		}
		c.cats[cat.ID] = ci
		for oi, obj := range cat.Objects {
			if _, dup := c.objects[obj.ID]; dup {
				return Catalog{}, fmt.Errorf("catalog: duplicate object id %q", obj.ID)
			}
			if _, clash := c.cats[obj.ID]; clash {
				return Catalog{}, fmt.Errorf("catalog: object id %q clashes with a category id", obj.ID)
			}
			c.objects[obj.ID] = objRef{ci, oi}
			for engine, ls := range obj.Engine {
				if c.labels[engine] == nil {
					c.labels[engine] = map[string]string{}
				}
				for _, l := range ls {
					if prev, dup := c.labels[engine][l]; dup {
						return Catalog{}, fmt.Errorf("catalog: %s label %q mapped to both %q and %q", engine, l, prev, obj.ID)
					}
					c.labels[engine][l] = obj.ID
				}
			}
		}
	}
	return c, nil
}

// Object returns an object by id.
func (c Catalog) Object(id string) (Object, bool) {
	r, ok := c.objects[id]
	if !ok {
		return Object{}, false
	}
	return c.Categories[r.cat].Objects[r.obj], true
}

// resolve expands object and category ids to object ids; the second result
// holds ids that are neither. Category members come back in catalog order.
func (c Catalog) resolve(ids []string) (objs map[string]bool, explicit map[string]bool, unknown []string) {
	objs, explicit = map[string]bool{}, map[string]bool{}
	for _, id := range ids {
		if ci, ok := c.cats[id]; ok {
			for _, o := range c.Categories[ci].Objects {
				objs[o.ID] = true
			}
			continue
		}
		if _, ok := c.objects[id]; ok {
			objs[id] = true
			explicit[id] = true
			continue
		}
		unknown = append(unknown, id)
	}
	return objs, explicit, unknown
}

// ExpandToEngine turns OpenVMS object or category ids into engine labels,
// sorted and deduplicated. Unknown ids are ignored.
func (c Catalog) ExpandToEngine(engine string, selection []string) []string {
	labels, _ := c.ExpandForModel(engine, selection, nil)
	return labels
}

// ExpandForModel is ExpandToEngine restricted to the labels the engine model
// supports. A nil available set means "unknown, assume all mapped labels".
// The second result lists requested ids that cannot be honored: unknown ids and
// explicitly requested objects with no usable engine label. Members reached
// only through a category id are skipped silently, so "all vehicles" never
// reports an object the engine cannot distinguish.
func (c Catalog) ExpandForModel(engine string, selection, available []string) (labels, unsupported []string) {
	objs, explicit, unknown := c.resolve(selection)
	unsupported = append(unsupported, unknown...)
	var avail map[string]bool
	if available != nil {
		avail = map[string]bool{}
		for _, l := range available {
			avail[l] = true
		}
	}
	set := map[string]bool{}
	for id := range objs {
		obj, _ := c.Object(id)
		n := 0
		for _, l := range obj.Engine[engine] {
			if avail != nil && !avail[l] {
				continue
			}
			set[l] = true
			n++
		}
		if n == 0 && explicit[id] {
			unsupported = append(unsupported, id)
		}
	}
	labels = make([]string, 0, len(set))
	for l := range set {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	sort.Strings(unsupported)
	return labels, slices.Compact(unsupported)
}

// CollapseFromEngine turns engine labels into a Selection. An object is
// selected when any of its labels is present; a category is fully selected
// only when every member that the engine can report is selected.
func (c Catalog) CollapseFromEngine(engine string, labels []string) Selection {
	sel := Selection{Objects: []string{}, Categories: []string{}, Raw: []string{}}
	picked := map[string]bool{}
	raw := map[string]bool{}
	for _, l := range labels {
		if id, ok := c.labels[engine][l]; ok {
			picked[id] = true
		} else {
			raw[l] = true
		}
	}
	for _, cat := range c.Categories {
		total, hit := 0, 0
		for _, o := range cat.Objects {
			if len(o.Engine[engine]) > 0 {
				total++
				if picked[o.ID] {
					hit++
				}
			}
			if picked[o.ID] {
				sel.Objects = append(sel.Objects, o.ID)
			}
		}
		if total > 0 && hit == total {
			sel.Categories = append(sel.Categories, cat.ID)
		}
	}
	for l := range raw {
		sel.Raw = append(sel.Raw, l)
	}
	sort.Strings(sel.Raw)
	return sel
}

// Classify normalizes one engine label for event display and search. The
// caller keeps the original label for audit and advanced search.
func (c Catalog) Classify(engine, label string) (objectID, category string, ok bool) {
	id, found := c.labels[engine][label]
	if !found {
		return "", "", false
	}
	return id, c.Categories[c.objects[id].cat].ID, true
}
