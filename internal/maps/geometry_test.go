package maps

import (
	"encoding/json"
	"math"
	"testing"
)

func TestValidatePolygon(t *testing.T) {
	t.Run("ValidGeoPolygon", func(t *testing.T) {
		raw := []byte(`{
			"type": "Polygon",
			"coordinates": [
				[
					[-64.19, -31.42],
					[-64.18, -31.42],
					[-64.18, -31.41],
					[-64.19, -31.41],
					[-64.19, -31.42]
				]
			]
		}`)
		pts, bbox, err := ValidatePolygon(raw, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pts) != 5 {
			t.Fatalf("expected 5 points, got %d", len(pts))
		}
		if bbox == nil || *bbox.MinLat != -31.42 || *bbox.MaxLat != -31.41 || *bbox.MinLng != -64.19 || *bbox.MaxLng != -64.18 {
			t.Fatalf("unexpected bbox: %+v", bbox)
		}
	})

	t.Run("ValidFloorPolygon", func(t *testing.T) {
		raw := []byte(`{
			"type": "Polygon",
			"coordinates": [
				[
					[0.1, 0.1],
					[0.8, 0.1],
					[0.8, 0.9],
					[0.1, 0.9],
					[0.1, 0.1]
				]
			]
		}`)
		pts, bbox, err := ValidatePolygon(raw, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pts) != 5 {
			t.Fatalf("expected 5 points, got %d", len(pts))
		}
		if bbox != nil {
			t.Fatalf("expected nil bbox for floor polygon, got %+v", bbox)
		}
	})

	t.Run("SelfIntersectingBowtie", func(t *testing.T) {
		raw := []byte(`{
			"type": "Polygon",
			"coordinates": [
				[
					[0.0, 0.0],
					[1.0, 1.0],
					[0.0, 1.0],
					[1.0, 0.0],
					[0.0, 0.0]
				]
			]
		}`)
		_, _, err := ValidatePolygon(raw, true)
		if err == nil {
			t.Fatal("expected error for self-intersecting polygon, got nil")
		}
	})

	t.Run("UnclosedRing", func(t *testing.T) {
		raw := []byte(`{
			"type": "Polygon",
			"coordinates": [
				[
					[0.1, 0.1],
					[0.8, 0.1],
					[0.8, 0.9],
					[0.1, 0.9]
				]
			]
		}`)
		_, _, err := ValidatePolygon(raw, true)
		if err == nil {
			t.Fatal("expected error for unclosed polygon ring, got nil")
		}
	})

	t.Run("TooFewCoordinates", func(t *testing.T) {
		raw := []byte(`{
			"type": "Polygon",
			"coordinates": [
				[
					[0.1, 0.1],
					[0.8, 0.1],
					[0.1, 0.1]
				]
			]
		}`)
		_, _, err := ValidatePolygon(raw, true)
		if err == nil {
			t.Fatal("expected error for < 4 coordinates, got nil")
		}
	})

	t.Run("TooManyCoordinates", func(t *testing.T) {
		coords := make([][]float64, 502)
		for i := 0; i < 501; i++ {
			coords[i] = []float64{0.1, 0.1}
		}
		coords[501] = []float64{0.1, 0.1}
		raw, _ := json.Marshal(map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{coords},
		})
		_, _, err := ValidatePolygon(raw, true)
		if err == nil {
			t.Fatal("expected error for > 500 vertices, got nil")
		}
	})

	t.Run("FloorOutOfBounds", func(t *testing.T) {
		raw := []byte(`{
			"type": "Polygon",
			"coordinates": [
				[
					[0.0, 0.0],
					[1.5, 0.0],
					[1.5, 1.0],
					[0.0, 0.0]
				]
			]
		}`)
		_, _, err := ValidatePolygon(raw, true)
		if err == nil {
			t.Fatal("expected error for floor coord > 1, got nil")
		}
	})

	t.Run("GeoOutOfBounds", func(t *testing.T) {
		raw := []byte(`{
			"type": "Polygon",
			"coordinates": [
				[
					[0.0, 0.0],
					[190.0, 0.0],
					[190.0, 10.0],
					[0.0, 0.0]
				]
			]
		}`)
		_, _, err := ValidatePolygon(raw, false)
		if err == nil {
			t.Fatal("expected error for lng > 180, got nil")
		}
	})
}

func TestHaversineMeters(t *testing.T) {
	// Obelisco Buenos Aires (-34.6037, -58.3816) to Plaza de Mayo (-34.6083, -58.3712)
	// Approximate distance: ~1.1 - 1.2 km
	dist := HaversineMeters(-34.6037, -58.3816, -34.6083, -58.3712)
	if dist < 1000 || dist > 1300 {
		t.Fatalf("expected distance ~1100m, got %.2fm", dist)
	}

	// Zero distance
	zero := HaversineMeters(-34.6037, -58.3816, -34.6037, -58.3816)
	if math.Abs(zero) > 1e-6 {
		t.Fatalf("expected 0 for identical points, got %f", zero)
	}
}
