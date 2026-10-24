package provisionlpr

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "provision-registry.json")
	r, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	return r, path
}

func TestRegistryStartsEmptyAndDoesNotEnforce(t *testing.T) {
	r, _ := newRegistry(t)
	snap := r.Snapshot()
	if snap.Enforce || len(snap.Cameras) != 0 || snap.LoadError != "" {
		t.Fatalf("snapshot = %+v, want an empty registry that does not enforce", snap)
	}
}

func TestRegistryPersistsCamerasAndEnforcementAcrossRestarts(t *testing.T) {
	r, path := newRegistry(t)
	if err := r.Upsert(RegisteredCamera{CameraID: "cam-a", IP: "10.0.15.107"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Upsert(RegisteredCamera{CameraID: "cam-b", IP: "10.0.15.108"}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnforce(true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("registry file = %v, %v; want mode 0600", info, err)
	}

	again, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	snap := again.Snapshot()
	if !snap.Enforce || len(snap.Cameras) != 2 || snap.Cameras[0].CameraID != "cam-a" || snap.Cameras[1].IP != "10.0.15.108" {
		t.Fatalf("after restart: %+v", snap)
	}
	if c, ok := again.Identify("10.0.15.107"); !ok || c.CameraID != "cam-a" {
		t.Fatalf("Identify = %+v, %v", c, ok)
	}
}

func TestRegistryIdentifiesByCanonicalAddress(t *testing.T) {
	r, _ := newRegistry(t)
	_ = r.Upsert(RegisteredCamera{CameraID: "cam-a", IP: "::ffff:10.0.15.107"})
	for _, ip := range []string{"10.0.15.107", "::ffff:10.0.15.107"} {
		if c, ok := r.Identify(ip); !ok || c.CameraID != "cam-a" || c.IP != "10.0.15.107" {
			t.Fatalf("Identify(%q) = %+v, %v", ip, c, ok)
		}
	}
	for _, ip := range []string{"10.0.15.108", "not-an-ip", ""} {
		if _, ok := r.Identify(ip); ok {
			t.Fatalf("Identify(%q) matched a camera that is not registered", ip)
		}
	}
}

func TestRegistryRejectsInvalidRegistrations(t *testing.T) {
	r, _ := newRegistry(t)
	for name, c := range map[string]RegisteredCamera{
		"empty id":      {CameraID: "", IP: "10.0.0.5"},
		"id with space": {CameraID: "cam a", IP: "10.0.0.5"},
		"id with slash": {CameraID: "../cam", IP: "10.0.0.5"},
		"id too long":   {CameraID: fmt.Sprintf("%0129d", 1), IP: "10.0.0.5"},
		"not an ip":     {CameraID: "cam", IP: "camera.local"},
		"unspecified":   {CameraID: "cam", IP: "0.0.0.0"},
		"multicast":     {CameraID: "cam", IP: "224.0.0.1"},
		"loopback":      {CameraID: "cam", IP: "127.0.0.1"},
	} {
		if err := r.Upsert(c); !errors.Is(err, ErrInvalidCamera) {
			t.Fatalf("%s: err = %v, want ErrInvalidCamera", name, err)
		}
	}
	if len(r.Snapshot().Cameras) != 0 {
		t.Fatal("an invalid registration was stored")
	}
}

func TestRegistryKeepsOneCameraPerAddressAndAllowsMovingACamera(t *testing.T) {
	r, _ := newRegistry(t)
	_ = r.Upsert(RegisteredCamera{CameraID: "cam-a", IP: "10.0.0.5"})
	if err := r.Upsert(RegisteredCamera{CameraID: "cam-b", IP: "10.0.0.5"}); !errors.Is(err, ErrIPInUse) {
		t.Fatalf("err = %v, want ErrIPInUse", err)
	}
	if err := r.Upsert(RegisteredCamera{CameraID: "cam-a", IP: "10.0.0.6"}); err != nil {
		t.Fatalf("moving a camera to a new address: %v", err)
	}
	if _, ok := r.Identify("10.0.0.5"); ok {
		t.Fatal("the old address still identifies the camera")
	}
	if err := r.Upsert(RegisteredCamera{CameraID: "cam-b", IP: "10.0.0.5"}); err != nil {
		t.Fatalf("the freed address could not be reused: %v", err)
	}
}

func TestRegistryRemoveForgetsTheCameraAndTolerantlyIgnoresUnknownOnes(t *testing.T) {
	r, path := newRegistry(t)
	_ = r.Upsert(RegisteredCamera{CameraID: "cam-a", IP: "10.0.0.5"})
	if err := r.Remove("cam-a"); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove("never-registered"); err != nil {
		t.Fatalf("removing an unknown camera: %v", err)
	}
	again, _ := LoadRegistry(path)
	if len(again.Snapshot().Cameras) != 0 {
		t.Fatal("a removed camera came back after a restart")
	}
}

func TestRegistryBoundsHowManyCamerasItHolds(t *testing.T) {
	r, _ := newRegistry(t)
	for i := 0; i < maxRegisteredCameras; i++ {
		if err := r.Upsert(RegisteredCamera{CameraID: fmt.Sprintf("cam-%d", i), IP: fmt.Sprintf("10.1.%d.%d", i/250+1, i%250+1)}); err != nil {
			t.Fatalf("camera %d: %v", i, err)
		}
	}
	if err := r.Upsert(RegisteredCamera{CameraID: "one-too-many", IP: "192.168.0.1"}); !errors.Is(err, ErrRegistryFull) {
		t.Fatalf("err = %v, want ErrRegistryFull", err)
	}
	if err := r.Upsert(RegisteredCamera{CameraID: "cam-0", IP: "192.168.0.2"}); err != nil {
		t.Fatalf("updating a registered camera when full: %v", err)
	}
}

func TestRegistryThatCannotBeReadRefusesEverythingAndCannotBeOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provision-registry.json")
	if err := os.WriteFile(path, []byte(`{"enforce": tru`), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := LoadRegistry(path)
	if err == nil {
		t.Fatal("a damaged registry loaded without an error")
	}
	if snap := r.Snapshot(); !snap.Enforce || len(snap.Cameras) != 0 || snap.LoadError == "" {
		t.Fatalf("snapshot = %+v, want an enforcing empty registry that names the problem", snap)
	}
	if err := r.SetEnforce(false); err == nil {
		t.Fatal("enforcement was turned off on a registry that failed to load")
	}
	data, _ := os.ReadFile(path) //nolint:gosec // path is a temp file this test created.
	if string(data) != `{"enforce": tru` {
		t.Fatalf("the damaged file was overwritten: %q", data)
	}
}

func TestRegistryRejectsAStoredCameraThatIsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provision-registry.json")
	if err := os.WriteFile(path, []byte(`{"enforce":false,"cameras":[{"camera_id":"cam","ip":"0.0.0.0"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := LoadRegistry(path)
	if err == nil || !r.Snapshot().Enforce {
		t.Fatalf("err = %v, enforce = %v; an invalid stored camera must lock the registry", err, r.Snapshot().Enforce)
	}
}

func TestRegistryRollsBackWhenItCannotSave(t *testing.T) {
	r, _ := newRegistry(t)
	_ = r.Upsert(RegisteredCamera{CameraID: "cam-a", IP: "10.0.0.5"})
	r.path = filepath.Join(t.TempDir(), "missing-dir", "registry.json") // saving will fail
	if err := r.Upsert(RegisteredCamera{CameraID: "cam-b", IP: "10.0.0.6"}); err == nil {
		t.Fatal("a failed save was reported as success")
	}
	if _, ok := r.Identify("10.0.0.6"); ok || len(r.Snapshot().Cameras) != 1 {
		t.Fatalf("the registration stayed in memory after the save failed: %+v", r.Snapshot())
	}
	if err := r.SetEnforce(true); err == nil || r.Enforcing() {
		t.Fatalf("enforcement changed in memory although it was not saved (err %v)", err)
	}
}

func TestRegistryTracksUnregisteredSourcesWithinABound(t *testing.T) {
	r, _ := newRegistry(t)
	base := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	for i := 0; i < maxUnregisteredSeen+20; i++ {
		r.NoteUnregistered(fmt.Sprintf("10.9.%d.%d", i/250, i%250), "/Vendor/Event", true, base.Add(time.Duration(i)*time.Second))
	}
	r.NoteUnregistered("10.9.0.255", "/SendKeepalive", false, base.Add(time.Hour))
	r.NoteUnregistered("10.9.0.255", "/Vendor/Event", true, base.Add(2*time.Hour))
	snap := r.Snapshot()
	if len(snap.Unregistered) != maxUnregisteredSeen {
		t.Fatalf("tracked %d sources, want the bound %d", len(snap.Unregistered), maxUnregisteredSeen)
	}
	top := snap.Unregistered[0]
	if top.IP != "10.9.0.255" || top.Requests != 2 || top.LastPath != "/Vendor/Event" || !top.Rejected || !top.FirstSeen.Equal(base.Add(time.Hour)) {
		t.Fatalf("most recent source = %+v", top)
	}
	// Registering an address takes it off the unregistered list.
	if err := r.Upsert(RegisteredCamera{CameraID: "cam", IP: "10.9.0.255"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range r.Snapshot().Unregistered {
		if s.IP == "10.9.0.255" {
			t.Fatal("a registered address is still listed as unregistered")
		}
	}
}
