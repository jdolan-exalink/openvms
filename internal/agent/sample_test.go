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

func TestAIDetectorHardwareProbing(t *testing.T) {
	root := t.TempDir()
	dev := filepath.Join(root, "dev")
	proc := filepath.Join(root, "proc")
	usb := filepath.Join(root, "usb")
	sys := filepath.Join(root, "sys")
	drm := filepath.Join(root, "drm")

	_ = os.MkdirAll(dev, 0o755)
	_ = os.MkdirAll(proc, 0o755)
	_ = os.MkdirAll(usb, 0o755)
	_ = os.MkdirAll(sys, 0o755)
	_ = os.MkdirAll(drm, 0o755)

	// Initially, none present
	paths := Paths{Dev: dev, Proc: proc, USB: usb, Sys: sys, DRM: drm, CCTV: root, DB: root}
	snap := Read(paths, "cpu")
	if snap.CoralUSB || snap.CoralPCIe || snap.Hailo || snap.Nvidia || snap.OpenVINO {
		t.Fatalf("expected all AI hardware absent, got: %+v", snap)
	}

	// 1. Coral PCIe (/dev/apex_0)
	_ = os.WriteFile(filepath.Join(dev, "apex_0"), []byte{}, 0o644)
	snap = Read(paths, "cpu")
	if !snap.CoralPCIe || !snap.Coral {
		t.Fatalf("expected Coral PCIe detected, got: %+v", snap)
	}
	_ = os.Remove(filepath.Join(dev, "apex_0"))

	// 2. Hailo-8 (/dev/hailo0)
	_ = os.WriteFile(filepath.Join(dev, "hailo0"), []byte{}, 0o644)
	snap = Read(paths, "cpu")
	if !snap.Hailo {
		t.Fatalf("expected Hailo-8 detected, got: %+v", snap)
	}
	_ = os.Remove(filepath.Join(dev, "hailo0"))

	// 3. NVIDIA (/dev/nvidia0)
	_ = os.WriteFile(filepath.Join(dev, "nvidia0"), []byte{}, 0o644)
	snap = Read(paths, "cpu")
	if !snap.Nvidia {
		t.Fatalf("expected NVIDIA detected, got: %+v", snap)
	}
	_ = os.Remove(filepath.Join(dev, "nvidia0"))

	// 4. OpenVINO via Intel CPU with AVX2 in proc/cpuinfo
	cpuinfo := "processor : 0\nvendor_id : GenuineIntel\nmodel name : 12th Gen Intel(R) Core(TM) i7-12700\nflags : fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr sse sse2 ss ht tm pbe syscall nx pdpe1gb rdtscp lm constant_tsc art arch_perfmon pebs bts rep_good nopl xtopology nonstop_tsc cpuid aperfmperf tsc_known_freq pni pclmulqdq dtes64 monitor ds_cpl vmx smx est tm2 ssse3 sdbg fma cx16 xtpr pdcm pcid sse4_1 sse4_2 x2apic movbe popcnt tsc_deadline_timer aes xsave avx f16c rdrand lahf_lm abm 3dnowprefetch cpuid_fault epb ssbd ibrs ibpb stibp ibrs_enhanced tpr_shadow flexpriority ept vpid ept_ad fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid rdseed adx smap clflushopt clwb intel_pt sha_ni xsaveopt xsavec xgetbv1 xsaves split_lock_detect avx_vnni dtherm ida arat pln pts hwp hwp_notify hwp_act_window hwp_epp hwp_pkg_req umip pku ospke waitpkg gfni vaes vpclmulqdq tdx_guest user_shstk md_clear serialize pconfig arch_lbr flush_l1d arch_capabilities\n"
	_ = os.WriteFile(filepath.Join(proc, "cpuinfo"), []byte(cpuinfo), 0o644)
	snap = Read(paths, "cpu")
	if !snap.OpenVINO {
		t.Fatalf("expected OpenVINO detected with Intel CPU + AVX2, got: %+v", snap)
	}
}

func TestReadIncludesStorageWatchdogFields(t *testing.T) {
	root := t.TempDir()
	snap := Read(Paths{CCTV: root, DB: root}, "cpu")
	if snap.StorageBufferPercent != 15 {
		t.Errorf("expected StorageBufferPercent=15, got %d", snap.StorageBufferPercent)
	}
	if !snap.StorageWatchdog {
		t.Errorf("expected StorageWatchdog=true by default")
	}
}
