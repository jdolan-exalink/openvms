package maps

import (
	"encoding/json"
	"fmt"
	"math"
)

// Point represents a 2D coordinate (lng/lat in geo, or x/y in floor plan).
type Point struct {
	X float64 // lng in geo, x in floor
	Y float64 // lat in geo, y in floor
}

// BBox holds geographic bounding box values.
type BBox struct {
	MinLat *float64
	MinLng *float64
	MaxLat *float64
	MaxLng *float64
}

type geoJSONGeometry struct {
	Type        string        `json:"type"`
	Coordinates [][][]float64 `json:"coordinates"`
}

func cross(o, a, b Point) float64 {
	return (a.X-o.X)*(b.Y-o.Y) - (a.Y-o.Y)*(b.X-o.X)
}

func segmentsIntersect(a, b, c, d Point) bool {
	d1 := cross(a, b, c)
	d2 := cross(a, b, d)
	d3 := cross(c, d, a)
	d4 := cross(c, d, b)
	return ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) && ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0))
}

// HasSelfIntersection reports whether any two non-adjacent edges of the polygon properly cross each other.
func HasSelfIntersection(points []Point) bool {
	n := len(points)
	if n < 4 {
		return false
	}
	verts := points
	// If first point equals last point, strip duplicate last point for edge checking
	if math.Abs(points[0].X-points[n-1].X) < 1e-9 && math.Abs(points[0].Y-points[n-1].Y) < 1e-9 {
		verts = points[:n-1]
	}
	nv := len(verts)
	if nv < 4 {
		return false
	}
	for i := 0; i < nv; i++ {
		for j := i + 1; j < nv; j++ {
			adjacent := j == i+1 || (i == 0 && j == nv-1)
			if adjacent {
				continue
			}
			if segmentsIntersect(verts[i], verts[(i+1)%nv], verts[j], verts[(j+1)%nv]) {
				return true
			}
		}
	}
	return false
}

// ValidatePolygon parses and validates a GeoJSON Polygon geometry.
func ValidatePolygon(raw []byte, isFloor bool) ([]Point, *BBox, error) {
	if len(raw) == 0 {
		return nil, nil, &ValidationError{Msg: "geometry is required"}
	}

	var geom geoJSONGeometry
	if err := json.Unmarshal(raw, &geom); err != nil {
		return nil, nil, &ValidationError{Msg: fmt.Sprintf("invalid GeoJSON: %v", err)}
	}

	if geom.Type != "Polygon" {
		return nil, nil, &ValidationError{Msg: "geometry type must be Polygon"}
	}
	if len(geom.Coordinates) == 0 || len(geom.Coordinates[0]) == 0 {
		return nil, nil, &ValidationError{Msg: "polygon coordinates ring cannot be empty"}
	}

	ring := geom.Coordinates[0]
	n := len(ring)
	if n < 4 {
		return nil, nil, &ValidationError{Msg: "polygon ring must contain at least 4 coordinates (triangle + closing point)"}
	}
	if n > 501 {
		return nil, nil, &ValidationError{Msg: "polygon vertices exceed maximum limit of 500"}
	}

	// Check closed ring
	first := ring[0]
	last := ring[n-1]
	if len(first) < 2 || len(last) < 2 {
		return nil, nil, &ValidationError{Msg: "each coordinate must contain [x, y] or [lng, lat]"}
	}
	if math.Abs(first[0]-last[0]) > 1e-9 || math.Abs(first[1]-last[1]) > 1e-9 {
		return nil, nil, &ValidationError{Msg: "polygon ring must be closed (first coordinate must equal last coordinate)"}
	}

	points := make([]Point, n)
	var minX, maxX, minY, maxY float64

	for i, coord := range ring {
		if len(coord) < 2 {
			return nil, nil, &ValidationError{Msg: "each coordinate must contain at least 2 numbers"}
		}
		x, y := coord[0], coord[1]
		if i == 0 {
			minX, maxX = x, x
			minY, maxY = y, y
		} else {
			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
		}

		if isFloor {
			if x < 0 || x > 1 || y < 0 || y > 1 {
				return nil, nil, &ValidationError{Msg: "floor polygon coordinates must be normalized between 0 and 1"}
			}
		} else {
			if x < -180 || x > 180 {
				return nil, nil, &ValidationError{Msg: "longitude must be between -180 and 180"}
			}
			if y < -90 || y > 90 {
				return nil, nil, &ValidationError{Msg: "latitude must be between -90 and 90"}
			}
		}

		points[i] = Point{X: x, Y: y}
	}

	if HasSelfIntersection(points) {
		return nil, nil, &ValidationError{Msg: "polygon has self-intersecting edges"}
	}

	var bbox *BBox
	if !isFloor {
		bbox = &BBox{
			MinLng: &minX,
			MaxLng: &maxX,
			MinLat: &minY,
			MaxLat: &maxY,
		}
	}

	return points, bbox, nil
}

// HaversineMeters calculates great-circle distance between two geo points in meters.
func HaversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusM = 6371000.0
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLng := (lng2 - lng1) * math.Pi / 180.0
	phi1 := lat1 * math.Pi / 180.0
	phi2 := lat2 * math.Pi / 180.0

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}
