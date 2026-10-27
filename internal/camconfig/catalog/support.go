package catalog

// Support says whether an engine model can report an object.
type Support int

// Support values. Unknown is the honest answer while the model's labels cannot be read.
const (
	SupportUnknown Support = iota
	SupportNo
	SupportYes
)

// SupportFor reports whether the engine can report obj. An object with no label mapped for
// the engine is never supported. With a nil available set (labels unreadable) a mapped
// object is Unknown, not Yes: the model may not have been trained for it.
func SupportFor(engine string, obj Object, available []string) Support {
	labels := obj.Engine[engine]
	if len(labels) == 0 {
		return SupportNo
	}
	if available == nil {
		return SupportUnknown
	}
	for _, l := range labels {
		for _, a := range available {
			if a == l {
				return SupportYes
			}
		}
	}
	return SupportNo
}

// HasCategory reports whether id is a category id.
func (c Catalog) HasCategory(id string) bool {
	_, ok := c.cats[id]
	return ok
}
