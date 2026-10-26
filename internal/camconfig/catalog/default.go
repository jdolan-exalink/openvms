package catalog

func fr(labels ...string) map[string][]string {
	return map[string][]string{EngineFrigate: labels}
}

// carrierLabels are the parcel-carrier classes of Frigate+ models.
var carrierLabels = []string{
	"package", "amazon", "ups", "fedex", "dhl", "usps", "an_post", "purolator",
	"postnl", "nzpost", "postnord", "gls", "dpd", "royal_mail", "canada_post",
}

// otherCOCO are COCO-80 labels with no OpenVMS surveillance meaning. They are
// kept in an explicit bucket instead of being dropped.
var otherCOCO = []string{
	"traffic light", "fire hydrant", "stop sign", "parking meter", "bench",
	"tie", "umbrella", "frisbee", "skis", "snowboard", "sports ball", "kite",
	"baseball bat", "baseball glove", "skateboard", "surfboard", "tennis racket",
	"bottle", "wine glass", "cup", "fork", "knife", "spoon", "bowl", "banana",
	"apple", "sandwich", "orange", "broccoli", "carrot", "hot dog", "pizza",
	"donut", "cake", "chair", "couch", "potted plant", "bed", "dining table",
	"toilet", "tv", "laptop", "mouse", "remote", "keyboard", "cell phone",
	"microwave", "oven", "toaster", "sink", "refrigerator", "book", "clock",
	"vase", "scissors", "teddy bear", "hair drier", "toothbrush",
}

// Default returns the built-in catalog. Order is stable: the UI renders it.
func Default() Catalog {
	c, err := New([]Category{
		{ID: "people", Label: "Personas", Objects: []Object{
			{ID: "person", Label: "Persona", Engine: fr("person")},
		}},
		{ID: "vehicle", Label: "Vehículos", Objects: []Object{
			{ID: "vehicle.car", Label: "Auto", Engine: fr("car")},
			// No Frigate label separates pickups from cars: capability absent.
			{ID: "vehicle.pickup", Label: "Camioneta"},
			{ID: "vehicle.truck", Label: "Camión", Engine: fr("truck")},
			{ID: "vehicle.bus", Label: "Bus", Engine: fr("bus")},
			{ID: "vehicle.motorcycle", Label: "Moto", Engine: fr("motorcycle")},
			{ID: "vehicle.bicycle", Label: "Bicicleta", Engine: fr("bicycle")},
			{ID: "vehicle.other", Label: "Otros", Engine: fr("airplane", "boat", "train")},
		}},
		{ID: "animal", Label: "Animales", Objects: []Object{
			{ID: "animal.dog", Label: "Perro", Engine: fr("dog")},
			{ID: "animal.cat", Label: "Gato", Engine: fr("cat")},
			{ID: "animal.bird", Label: "Ave", Engine: fr("bird")},
			{ID: "animal.horse", Label: "Caballo", Engine: fr("horse")},
			{ID: "animal.other", Label: "Otros", Engine: fr("sheep", "cow", "elephant", "bear", "zebra", "giraffe")},
		}},
		{ID: "package", Label: "Paquetes y objetos", Objects: []Object{
			{ID: "package.box", Label: "Paquetes / Cajas", Engine: fr(carrierLabels...)},
			{ID: "package.bag", Label: "Bolsos / Mochilas", Engine: fr("backpack", "handbag", "suitcase")},
		}},
		{ID: "safety", Label: "Seguridad / EPP", Objects: []Object{
			{ID: "safety.helmet", Label: "Casco", RequiresModel: true, Engine: fr("helmet")},
			{ID: "safety.vest", Label: "Chaleco", RequiresModel: true, Engine: fr("vest")},
			{ID: "safety.other", Label: "Otros", RequiresModel: true},
		}},
		{ID: "other", Label: "Otros", Objects: []Object{
			// LPR and face are analytics modules (see U6); listed so their
			// labels classify cleanly instead of falling to Raw.
			{ID: "other.license_plate", Label: "Matrícula", Engine: fr("license_plate")},
			{ID: "other.face", Label: "Rostro", Engine: fr("face")},
			{ID: "other.misc", Label: "Otros objetos", Engine: fr(otherCOCO...)},
		}},
	})
	if err != nil {
		panic(err) // static data; covered by tests
	}
	return c
}
