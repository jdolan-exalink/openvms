package maps_test

import (
	"context"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/maps"
)

func TestDefaultConfig(t *testing.T) {
	cfg := maps.DefaultConfig()
	if cfg.Provider.ID != "openfreemap" {
		t.Fatalf("expected openfreemap, got %s", cfg.Provider.ID)
	}
	if cfg.Provider.Kind != "vector-style" {
		t.Fatalf("expected vector-style, got %s", cfg.Provider.Kind)
	}
	if cfg.Provider.Offline {
		t.Fatal("expected offline to be false for hosted tiles")
	}
	if len(cfg.Provider.Tiles) != 0 {
		t.Fatalf("vector-style carries no tile list, got %v", cfg.Provider.Tiles)
	}
	if cfg.Provider.StyleURLLight == nil || *cfg.Provider.StyleURLLight != "https://tiles.openfreemap.org/styles/liberty" {
		t.Fatalf("unexpected light style: %v", cfg.Provider.StyleURLLight)
	}
	if cfg.Provider.StyleURLDark == nil || *cfg.Provider.StyleURLDark != "https://tiles.openfreemap.org/styles/dark" {
		t.Fatalf("unexpected dark style: %v", cfg.Provider.StyleURLDark)
	}
	// Latin America is the product's home ground: the default view must land there
	// instead of null island.
	if cfg.DefaultCenter.Lat != -14.2 || cfg.DefaultCenter.Lng != -51.9 {
		t.Fatalf("unexpected default center: %+v", cfg.DefaultCenter)
	}
	if cfg.DefaultZoom != 3 {
		t.Fatalf("expected default zoom 3, got %v", cfg.DefaultZoom)
	}

	svc := &maps.Service{Config: cfg}
	got, err := svc.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("GetConfig returned error: %v", err)
	}
	if got.Provider.ID != cfg.Provider.ID {
		t.Fatalf("expected %s, got %s", cfg.Provider.ID, got.Provider.ID)
	}
}
