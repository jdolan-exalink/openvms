package inventory

import (
	"slices"
	"strings"
	"testing"
)

func TestNormalizeCameraUpdate(t *testing.T) {
	t.Run("trims text and dedupes tags", func(t *testing.T) {
		in := CameraUpdate{
			Description: ptr("  Entrada principal  "),
			Location:    ptr(" Planta baja "),
			Tags:        ptr([]string{" acceso ", "Acceso", "", "  ", "exterior", "acceso"}),
		}
		if err := normalizeCameraUpdate(&in); err != nil {
			t.Fatal(err)
		}
		if *in.Description != "Entrada principal" || *in.Location != "Planta baja" {
			t.Errorf("text not trimmed: %q %q", *in.Description, *in.Location)
		}
		if want := []string{"acceso", "exterior"}; !slices.Equal(*in.Tags, want) {
			t.Errorf("tags = %v, want %v", *in.Tags, want)
		}
	})

	t.Run("empty tag list stays an empty non-nil slice", func(t *testing.T) {
		in := CameraUpdate{Tags: ptr([]string{})}
		if err := normalizeCameraUpdate(&in); err != nil {
			t.Fatal(err)
		}
		if in.Tags == nil || *in.Tags == nil {
			t.Error("tags must clear to an empty slice, not nil")
		}
	})

	t.Run("accepts sub and main", func(t *testing.T) {
		for _, q := range []string{"sub", "main"} {
			in := CameraUpdate{DefaultLiveQuality: ptr(q)}
			if err := normalizeCameraUpdate(&in); err != nil {
				t.Errorf("%s: %v", q, err)
			}
		}
	})

	rejects := map[string]CameraUpdate{
		"unknown quality":      {DefaultLiveQuality: ptr("hd")},
		"description too long": {Description: ptr(strings.Repeat("a", 1001))},
		"location too long":    {Location: ptr(strings.Repeat("a", 201))},
		"too many tags": {Tags: ptr(func() []string {
			out := make([]string, 21)
			for i := range out {
				out[i] = "t" + strings.Repeat("x", i)
			}
			return out
		}())},
		"tag too long": {Tags: ptr([]string{strings.Repeat("a", 41)})},
	}
	for name, in := range rejects {
		t.Run("rejects "+name, func(t *testing.T) {
			if err := normalizeCameraUpdate(&in); err == nil {
				t.Error("want validation error")
			}
		})
	}
}
