package agent

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFilesystemBytesRejectsInvalidBlockSizesAndSaturatesOverflow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		blocks uint64
		size   int64
		want   uint64
	}{
		{name: "negative block size", blocks: 10, size: -1, want: 0},
		{name: "zero block size", blocks: 10, size: 0, want: 0},
		{name: "normal", blocks: 5, size: 4096, want: 20480},
		{name: "overflow saturates", blocks: math.MaxUint64, size: 2, want: math.MaxUint64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := filesystemBytes(tc.blocks, tc.size); got != tc.want {
				t.Fatalf("filesystemBytes(%d, %d) = %d, want %d", tc.blocks, tc.size, got, tc.want)
			}
		})
	}
}

func TestCoralPresentFromSysfs(t *testing.T) {
	root := t.TempDir()
	dev := filepath.Join(root, "dev")
	usb := filepath.Join(root, "usb", "1-1")
	if err := os.MkdirAll(usb, 0o750); err != nil {
		t.Fatal(err)
	}
	// #nosec G306 -- test fixture deliberately uses public or executable permissions.
	if err := os.WriteFile(filepath.Join(usb, "idVendor"), []byte("18d1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// #nosec G306 -- test fixture deliberately uses public or executable permissions.
	if err := os.WriteFile(filepath.Join(usb, "idProduct"), []byte("9302\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !coralPresent(Paths{Dev: dev, USB: filepath.Join(root, "usb")}) {
		t.Fatal("expected coral")
	}
}

func TestNTPPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "udp")
	body := "  sl  local_address rem_address\n   1: 00000000:007B 00000000:0000 07\n"
	// #nosec G306 -- test fixture deliberately uses public or executable permissions.
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if !udpPort(path, 123) {
		t.Fatal("expected ntp port")
	}
}

func TestSamplerCachesForASecond(t *testing.T) {
	s := NewSampler(Paths{Proc: t.TempDir(), DRM: t.TempDir(), USB: t.TempDir(), Dev: t.TempDir(), CCTV: t.TempDir(), DB: t.TempDir()})
	first := s.Current("cpu")
	cpu := 42.0
	s.snap.CPUPercent = &cpu
	second := s.Current("cpu")
	if second.CPUPercent == nil || *second.CPUPercent != 42 || first.Version != Version {
		t.Fatalf("cache or version mismatch: %#v %#v", first, second)
	}
}

func TestCachedSnapshotDoesNotExposeMutableNetworkSlice(t *testing.T) {
	s := NewSampler(Paths{})
	s.at = time.Now()
	s.snap.Network = []NetworkInterface{{Name: "eth0", RXBytesPerSecond: 1}}
	first := s.Current("cpu")
	first.Network[0].Name = "caller-mutated"
	second := s.Current("cpu")
	if second.Network[0].Name != "eth0" {
		t.Fatalf("caller mutated cached sample: %#v", second.Network)
	}
}

func TestReadLeavesUnavailableCPUAbsent(t *testing.T) {
	root := t.TempDir()
	snapshot := Read(Paths{Proc: root, DRM: t.TempDir(), USB: t.TempDir(), Dev: t.TempDir(), CCTV: t.TempDir(), DB: t.TempDir()}, "cpu")
	if snapshot.CPUPercent != nil {
		t.Fatalf("missing proc CPU counters must remain unavailable: %v", *snapshot.CPUPercent)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"cpu_percent"`) {
		t.Fatalf("unavailable CPU must be omitted from the metrics contract: %s", encoded)
	}
}
