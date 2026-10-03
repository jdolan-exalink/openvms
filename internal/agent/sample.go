package agent

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jdolan-exalink/openvms/internal/platform/hoststat"
)

// Snapshot is what the edge agent reports once a second.
type Snapshot struct {
	Version             string  `json:"version"`
	Variant             string  `json:"variant"`
	NTP                 string  `json:"ntp"`
	CPUPercent          float64 `json:"cpu_percent"`
	MemoryTotal         uint64  `json:"memory_total_bytes"`
	MemoryAvailable     uint64  `json:"memory_available_bytes"`
	Coral               bool    `json:"coral"`
	GPUPresent          bool    `json:"gpu_present"`
	GPUVendor           string  `json:"gpu_vendor"`
	GPUName             string  `json:"gpu_name"`
	CCTVTotal           uint64  `json:"cctv_total_bytes"`
	CCTVFree            uint64  `json:"cctv_free_bytes"`
	DatabaseTotal       uint64  `json:"database_total_bytes"`
	DatabaseFree        uint64  `json:"database_free_bytes"`
}

// Paths are the files a sample reads. Empty fields use this machine.
type Paths struct {
	Proc string
	DRM  string
	USB  string
	Dev  string
	CCTV string
	DB   string
}

// Sampler caches one reading so overlapping polls do not stack the CPU sample.
type Sampler struct {
	mu   sync.Mutex
	at   time.Time
	snap Snapshot
	opts Paths
}

// NewSampler returns a sampler for the given paths.
func NewSampler(opts Paths) *Sampler {
	if opts.Proc == "" {
		opts.Proc = "/proc"
	}
	if opts.DRM == "" {
		opts.DRM = "/sys/class/drm"
	}
	if opts.USB == "" {
		opts.USB = "/sys/bus/usb/devices"
	}
	if opts.Dev == "" {
		opts.Dev = "/dev"
	}
	if opts.CCTV == "" {
		opts.CCTV = "/opt/openvms/frigate/storage"
	}
	if opts.DB == "" {
		opts.DB = "/"
	}
	return &Sampler{opts: opts}
}

// Current returns a snapshot no older than one second.
func (s *Sampler) Current(variant string) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.at.IsZero() && time.Since(s.at) < time.Second {
		return s.snap
	}
	s.snap = Read(s.opts, variant)
	s.at = time.Now()
	return s.snap
}

// Read samples the machine once.
func Read(opts Paths, variant string) Snapshot {
	if opts.Proc == "" {
		opts.Proc = "/proc"
	}
	host := hoststat.Read(hoststat.Roots{Proc: opts.Proc, DRM: opts.DRM, Disk: opts.DB})
	cctvTotal, cctvFree := diskUsage(opts.CCTV)
	dbTotal, dbFree := host.DiskTotal, host.DiskFree
	if opts.DB != "" && opts.DB != host.DiskPath {
		dbTotal, dbFree = diskUsage(opts.DB)
	}
	out := Snapshot{
		Version:         Version,
		Variant:         variant,
		NTP:             ntpState(opts.Proc),
		CPUPercent:      host.CPUPercent,
		MemoryTotal:     host.MemTotal,
		MemoryAvailable: host.MemAvailable,
		Coral:           coralPresent(opts),
		CCTVTotal:       cctvTotal,
		CCTVFree:        cctvFree,
		DatabaseTotal:   dbTotal,
		DatabaseFree:    dbFree,
	}
	if len(host.GPUs) > 0 {
		out.GPUPresent = true
		out.GPUVendor = host.GPUs[0].Vendor
		out.GPUName = host.GPUs[0].Name
	}
	return out
}

func coralPresent(opts Paths) bool {
	if opts.Dev == "" {
		opts.Dev = "/dev"
	}
	if _, err := os.Stat(filepath.Join(opts.Dev, "apex_0")); err == nil {
		return true
	}
	root := opts.USB
	if root == "" {
		root = "/sys/bus/usb/devices"
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, e := range entries {
		vendor := strings.TrimSpace(readText(filepath.Join(root, e.Name(), "idVendor")))
		product := strings.TrimSpace(readText(filepath.Join(root, e.Name(), "idProduct")))
		if (vendor == "1a6e" && product == "089a") || (vendor == "18d1" && product == "9302") {
			return true
		}
	}
	return false
}

func ntpState(proc string) string {
	if udpPort(filepath.Join(proc, "net/udp"), 123) || udpPort(filepath.Join(proc, "net/udp6"), 123) {
		return "active"
	}
	return "inactive"
}

func udpPort(path string, port int) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	want := strings.ToUpper(hexPort(port))
	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		local := fields[1]
		if i := strings.LastIndexByte(local, ':'); i >= 0 && strings.EqualFold(local[i+1:], want) {
			return true
		}
	}
	return false
}

func hexPort(port int) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[(port>>12)&0xF], digits[(port>>8)&0xF], digits[(port>>4)&0xF], digits[port&0xF]})
}

func diskUsage(path string) (total, free uint64) {
	var st syscall.Statfs_t
	if path == "" {
		return 0, 0
	}
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	return uint64(st.Blocks) * uint64(st.Bsize), uint64(st.Bavail) * uint64(st.Bsize)
}

func readText(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
