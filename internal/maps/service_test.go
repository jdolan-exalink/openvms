package maps_test

import (
	"context"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/maps"
)

func TestDefaultConfig(t *testing.T) {
	cfg := maps.DefaultConfig()
	if cfg.Provider.ID != "protomaps-local" {
		t.Fatalf("expected protomaps-local, got %s", cfg.Provider.ID)
	}
	if cfg.Provider.Kind != "pmtiles" {
		t.Fatalf("expected pmtiles, got %s", cfg.Provider.Kind)
	}
	if !cfg.Provider.Offline {
		t.Fatal("expected offline to be true by default")
	}
	if len(cfg.Provider.Tiles) == 0 || cfg.Provider.Tiles[0] != "/tiles/world.pmtiles" {
		t.Fatalf("unexpected tiles: %v", cfg.Provider.Tiles)
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
