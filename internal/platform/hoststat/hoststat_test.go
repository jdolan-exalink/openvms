package hoststat

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemBytesRejectsInvalidBlockSizeAndSaturatesOverflow(t *testing.T) {
	for _, tt := range []struct {
		name   string
		blocks uint64
		size   int64
		want   uint64
	}{
		{name: "negative size", blocks: 1, size: -1, want: 0},
		{name: "zero size", blocks: 1, size: 0, want: 0},
		{name: "overflow", blocks: math.MaxUint64, size: 2, want: math.MaxUint64},
		{name: "ordinary", blocks: 10, size: 4096, want: 40960},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := filesystemBytes(tt.blocks, tt.size); got != tt.want {
				t.Fatalf("filesystemBytes(%d, %d) = %d, want %d", tt.blocks, tt.size, got, tt.want)
			}
		})
	}
}

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
	if err := os.MkdirAll(dev, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dev, "vendor"), []byte("0x8086\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dev, "device"), []byte("0x56a0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := gpus(dir)
	if len(got) != 1 || got[0].Vendor != "intel" {
		t.Fatalf("%+v", got)
	}
}
