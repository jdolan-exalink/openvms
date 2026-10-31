package events

import "testing"

func TestNormalizeVehicleColor(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", "unknown"},
		{"unknown", "unknown"},
		{"0", "unknown"},
		{"Desconocido", "unknown"},
		{"1", "white"},
		{"Blanco", "white"},
		{"white", "white"},
		{"WHITE", "white"},
		{"2", "gray"},
		{"Gris", "gray"},
		{"grey", "gray"},
		{"3", "yellow"},
		{"Amarillo", "yellow"},
		{"4", "purple"},
		{"Rosa", "purple"},
		{"5", "red"},
		{"Rojo", "red"},
		{"red", "red"},
		{"6", "green"},
		{"Verde", "green"},
		{"7", "blue"},
		{"Azul", "blue"},
		{"8", "black"},
		{"Negro", "black"},
		{"black", "black"},
		{"Plateado", "silver"},
		{"silver", "silver"},
		{"Naranja", "orange"},
		{"orange", "orange"},
		{"Marron", "brown"},
		{"marrón", "brown"},
		{"Beige", "beige"},
	}

	for _, tc := range cases {
		got := NormalizeVehicleColor(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeVehicleColor(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeVehicleType(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", "unknown"},
		{"unknown", "unknown"},
		{"car", "car"},
		{"auto", "car"},
		{"coche", "car"},
		{"vehiculo", "car"},
		{"vehículo", "car"},
		{"sedan", "sedan"},
		{"suv", "suv"},
		{"pickup", "pickup"},
		{"camioneta", "pickup"},
		{"van", "van"},
		{"truck", "truck"},
		{"camión", "truck"},
		{"moto", "motorcycle"},
		{"motorcycle", "motorcycle"},
		{"bus", "bus"},
		{"colectivo", "bus"},
	}

	for _, tc := range cases {
		got := NormalizeVehicleType(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeVehicleType(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
