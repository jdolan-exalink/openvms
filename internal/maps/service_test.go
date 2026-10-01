package maps_test

import (
	"context"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/maps"
)

func TestDefaultConfig(t *testing.T) {
	cfg := maps.DefaultConfig()
	if cfg.Provider.ID != "osm-public" {
		t.Fatalf("expected osm-public, got %s", cfg.Provider.ID)
	}
	if cfg.Provider.Kind != "raster" {
		t.Fatalf("expected raster, got %s", cfg.Provider.Kind)
	}
	if cfg.Provider.Offline {
		t.Fatal("expected offline to be false for public tiles")
	}
	if len(cfg.Provider.Tiles) == 0 || cfg.Provider.Tiles[0] != "https://tile.openstreetmap.org/{z}/{x}/{y}.png" {
		t.Fatalf("unexpected tiles: %v", cfg.Provider.Tiles)
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
