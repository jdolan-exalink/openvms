package hoststat

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// GPU is a graphics device visible to this process.
type GPU struct {
	Vendor string
	Name   string
}

// Snapshot is the machine the service can see.
type Snapshot struct {
	CPUModel     string
	CPUOnline    int
	CPUPercent   float64
	MemTotal     uint64
	MemAvailable uint64
	DiskPath     string
	DiskTotal    uint64
	DiskFree     uint64
	GPUs         []GPU
	OpenVINO     bool
}

// Roots overrides the files Read uses. Empty fields fall back to this host.
type Roots struct {
	Proc string
	DRM  string
	Disk string
}

// Read samples CPU, memory, disk, GPUs and whether OpenVINO is installed.
func Read(roots Roots) Snapshot {
	if roots.Proc == "" {
		roots.Proc = "/proc"
	}
	if roots.DRM == "" {
		roots.DRM = "/sys/class/drm"
	}
	if roots.Disk == "" {
		roots.Disk = "/"
	}
	out := Snapshot{DiskPath: roots.Disk}
	out.CPUModel = cpuModel(roots.Proc)
	out.CPUOnline = cpuOnline(roots.Proc)
	out.CPUPercent = cpuPercent(roots.Proc)
	out.MemTotal, out.MemAvailable = memInfo(roots.Proc)
	out.DiskTotal, out.DiskFree = disk(roots.Disk)
	out.GPUs = gpus(roots.DRM)
	out.OpenVINO = openvinoInstalled()
	return out
}

func cpuModel(proc string) string {
	f, err := os.Open(filepath.Join(proc, "cpuinfo"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "model name") {
			if i := strings.IndexByte(line, ':'); i >= 0 {
				return strings.TrimSpace(line[i+1:])
			}
		}
	}
	return ""
}

func cpuOnline(proc string) int {
	b, err := os.ReadFile(filepath.Join(proc, "stat"))
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] >= '0' && line[3] <= '9' {
			n++
		}
	}
	return n
}

func cpuPercent(proc string) float64 {
	a := cpuTimes(proc)
	time.Sleep(200 * time.Millisecond)
	b := cpuTimes(proc)
	idle := b.idle - a.idle
	total := b.total - a.total
	if total <= 0 {
		return 0
	}
	used := float64(total-idle) / float64(total) * 100
	if used < 0 {
		return 0
	}
	if used > 100 {
		return 100
	}
	return used
}

type cpuSample struct{ idle, total uint64 }

func cpuTimes(proc string) cpuSample {
	b, err := os.ReadFile(filepath.Join(proc, "stat"))
	if err != nil {
		return cpuSample{}
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		var total, idle uint64
		for i := 1; i < len(fields); i++ {
			v, _ := strconv.ParseUint(fields[i], 10, 64)
			total += v
			if i == 4 || i == 5 {
				idle += v
			}
		}
		return cpuSample{idle: idle, total: total}
	}
	return cpuSample{}
}

func memInfo(proc string) (total, available uint64) {
	f, err := os.Open(filepath.Join(proc, "meminfo"))
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		v *= 1024
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			total = v
		case "MemAvailable":
			available = v
		}
	}
	return total, available
}

func disk(path string) (total, free uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil && path != "/" {
		if err2 := syscall.Statfs("/", &st); err2 != nil {
			return 0, 0
		}
	} else if err != nil {
		return 0, 0
	}
	return uint64(st.Blocks) * uint64(st.Bsize), uint64(st.Bavail) * uint64(st.Bsize)
}

func gpus(drm string) []GPU {
	entries, err := os.ReadDir(drm)
	if err != nil {
		return nil
	}
	var out []GPU
	seen := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "card") || strings.Contains(name, "-") {
			continue
		}
		dev := filepath.Join(drm, name, "device")
		vendor := strings.TrimSpace(readFile(filepath.Join(dev, "vendor")))
		device := strings.TrimSpace(readFile(filepath.Join(dev, "device")))
		key := vendor + device
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, GPU{Vendor: vendorName(vendor), Name: deviceName(vendor, device)})
	}
	return out
}

func vendorName(vendor string) string {
	switch strings.ToLower(vendor) {
	case "0x8086":
		return "intel"
	case "0x10de":
		return "nvidia"
	case "0x1002":
		return "amd"
	default:
		if vendor == "" {
			return ""
		}
		return vendor
	}
}

func deviceName(vendor, device string) string {
	switch vendorName(vendor) {
	case "intel":
		return "Intel " + device
	case "nvidia":
		return "NVIDIA " + device
	case "amd":
		return "AMD " + device
	default:
		return strings.TrimSpace(vendor + " " + device)
	}
}

func openvinoInstalled() bool {
	candidates := []string{
		"/usr/lib/x86_64-linux-gnu/libopenvino.so",
		"/usr/lib/libopenvino.so",
		"/opt/intel/openvino",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return os.Getenv("INTEL_OPENVINO_DIR") != ""
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
