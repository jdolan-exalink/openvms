package hoststat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSeesThisMachine(t *testing.T) {
	got := Read(Roots{})
	if got.CPUOnline < 1 {
		t.Fatalf("online cpus %d", got.CPUOnline)
	}
	if got.MemTotal == 0 {
		t.Fatal("no memory")
	}
	if got.DiskTotal == 0 {
		t.Fatal("no disk")
	}
}

func TestGPUsReadsVendorFiles(t *testing.T) {
	dir := t.TempDir()
	dev := filepath.Join(dir, "card0", "device")
	if err := os.MkdirAll(dev, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dev, "vendor"), []byte("0x8086\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dev, "device"), []byte("0x56a0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := gpus(dir)
	if len(got) != 1 || got[0].Vendor != "intel" {
		t.Fatalf("%+v", got)
	}
}
