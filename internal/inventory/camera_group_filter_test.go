package inventory

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNormalizeCameraGroupFilter(t *testing.T) {
	site := uuid.New()
	bad := []struct {
		name string
		in   CameraGroupFilter
	}{
		{"empty filter", CameraGroupFilter{}},
		{"blank name only", CameraGroupFilter{NameContains: "   "}},
		{"blank tag only", CameraGroupFilter{Tags: []string{" ", ""}}},
		{"nil site id", CameraGroupFilter{SiteIDs: []uuid.UUID{uuid.Nil}}},
		{"name too long", CameraGroupFilter{NameContains: strings.Repeat("a", maxGroupFilterName+1)}},
		{"too many tags", CameraGroupFilter{Tags: make([]string, maxGroupFilterValues+1)}},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			var ve *ValidationError
			if _, err := normalizeCameraGroupFilter(c.in); !errors.As(err, &ve) {
				t.Fatalf("err = %v, want ValidationError", err)
			}
		})
	}

	got, err := normalizeCameraGroupFilter(CameraGroupFilter{
		SiteIDs: []uuid.UUID{site, site}, Tags: []string{" Exterior ", "exterior", "Acceso"}, NameContains: "  Norte ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.SiteIDs) != 1 || len(got.Tags) != 2 || got.Tags[0] != "Exterior" || got.NameContains != "Norte" {
		t.Fatalf("normalized = %+v", got)
	}
}
