package vehicle

// SourceLabels are the Frigate classes that enqueue enrichment.
var SourceLabels = map[string]struct{}{
	"car":        {},
	"truck":      {},
	"bus":        {},
	"motorcycle": {},
}

// TypeMinConfidence is the floor below which the UI should not state a type.
const TypeMinConfidence = 0.60

// TypeFromLabels maps the detector label onto the OpenVMS taxonomy.
// The first vehicle label is the subject. A later car does not turn a
// motorcycle into an auto, and a later motorcycle does not turn a car into a
// moto. SUV and pickup are not guessed. Bicycle is not a vehicle type.
func TypeFromLabels(labels []string) (string, float32) {
	for _, label := range labels {
		switch label {
		case "bus":
			return "bus", 0.75
		case "truck":
			return "truck", 0.75
		case "car", "car-verified":
			return "car", 0.75
		case "motorcycle":
			return "motorcycle", 0.8
		}
	}
	return "unknown", 0
}

// SubjectType is the vehicle inside the snapshot. Frigate's green box is the
// car, truck or bus the photo shows. A motorcycle listed first must not take
// that box: the rider can share the event with the car that is actually framed.
func SubjectType(labels []string, greenBox bool) (string, float32) {
	if greenBox {
		for _, label := range labels {
			switch label {
			case "truck":
				return "truck", 0.75
			case "bus":
				return "bus", 0.75
			case "car", "car-verified":
				return "car", 0.75
			}
		}
	}
	return TypeFromLabels(labels)
}

// IsPerson reports whether a person clothing job should be enqueued.
func IsPerson(labels []string) bool {
	for _, label := range labels {
		if label == "person" {
			return true
		}
	}
	return false
}

// IsVehicle reports whether any label should enqueue enrichment.
func IsVehicle(labels []string) bool {
	for _, label := range labels {
		if _, ok := SourceLabels[label]; ok {
			return true
		}
	}
	return false
}
