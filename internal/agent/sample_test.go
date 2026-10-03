package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCoralPresentFromSysfs(t *testing.T) {
	root := t.TempDir()
	dev := filepath.Join(root, "dev")
	usb := filepath.Join(root, "usb", "1-1")
	if err := os.MkdirAll(usb, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(usb, "idVendor"), []byte("18d1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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
	s.snap.CPUPercent = 42
	second := s.Current("cpu")
	if second.CPUPercent != 42 || first.Version != Version {
		t.Fatalf("cache or version mismatch: %#v %#v", first, second)
	}
}
