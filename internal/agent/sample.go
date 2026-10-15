package agent

import (
	"bufio"
	"math"
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
	Version          string             `json:"version"`
	Variant          string             `json:"variant"`
	NTP              string             `json:"ntp"`
	CPUPercent       *float64           `json:"cpu_percent,omitempty"`
	MemoryTotal      uint64             `json:"memory_total_bytes"`
	MemoryAvailable  uint64             `json:"memory_available_bytes"`
	Coral            bool               `json:"coral"`
	CoralUSB         bool               `json:"coral_usb"`
	CoralPCIe        bool               `json:"coral_pcie"`
	OpenVINO         bool               `json:"openvino"`
	Hailo            bool               `json:"hailo"`
	Nvidia           bool               `json:"nvidia"`
	GPUPresent       bool               `json:"gpu_present"`
	GPUVendor        string             `json:"gpu_vendor"`
	GPUName          string             `json:"gpu_name"`
	CCTVTotal        uint64             `json:"cctv_total_bytes"`
	CCTVFree         uint64             `json:"cctv_free_bytes"`
	DatabaseTotal        uint64             `json:"database_total_bytes"`
	DatabaseFree         uint64             `json:"database_free_bytes"`
	StorageBufferPercent int                `json:"storage_buffer_percent"`
	StorageWatchdog      bool               `json:"storage_watchdog"`
	StorageOver85Percent bool               `json:"storage_over_85_percent"`
	DatabaseCritical     bool               `json:"database_critical"`
	PrunedSegmentsCount  uint64             `json:"pruned_segments_count"`
	Network              []NetworkInterface `json:"network_interfaces,omitempty"`
	NetworkSampledAt     *time.Time         `json:"network_sampled_at,omitempty"`
	SampledAt            *time.Time         `json:"sampled_at,omitempty"`
	OS                   string             `json:"os,omitempty"`
}

// Paths are the files a sample reads. Empty fields use this machine.
type Paths struct {
	Proc string
	DRM  string
	USB  string
	Dev  string
	CCTV string
	DB   string
	Etc  string
	Sys  string
}

// Sampler caches one reading so overlapping polls do not stack the CPU sample.
type Sampler struct {
	mu      sync.Mutex
	at      time.Time
	snap    Snapshot
	opts    Paths
	network *NetworkSampler
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
		opts.CCTV = "/mnt/cctv"
	}
	if opts.DB == "" {
		opts.DB = "/opt/frigate/config"
	}
	return &Sampler{opts: opts, network: newProcNetworkSampler(opts.Proc, opts.DB)}
}

// Current returns a snapshot no older than one second.
func (s *Sampler) Current(variant string) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.at.IsZero() && time.Since(s.at) < time.Second {
		return cloneSnapshot(s.snap)
	}
	prevNetwork := s.snap.Network
	prevNetworkAt := s.snap.NetworkSampledAt
	s.snap = Read(s.opts, variant)
	network := s.network.Sample()
	if network.SampledAt != nil {
		s.snap.Network = append([]NetworkInterface(nil), network.Interfaces...)
		s.snap.NetworkSampledAt = network.SampledAt
	} else {
		s.snap.Network = prevNetwork
		s.snap.NetworkSampledAt = prevNetworkAt
	}
	s.at = time.Now()
	return cloneSnapshot(s.snap)
}

func cloneSnapshot(in Snapshot) Snapshot {
	out := in
	out.Network = append([]NetworkInterface(nil), in.Network...)
	if in.CPUPercent != nil {
		cpu := *in.CPUPercent
		out.CPUPercent = &cpu
	}
	if in.NetworkSampledAt != nil {
		sampledAt := *in.NetworkSampledAt
		out.NetworkSampledAt = &sampledAt
	}
	if in.SampledAt != nil {
		sampledAt := *in.SampledAt
		out.SampledAt = &sampledAt
	}
	return out
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
	now := time.Now().UTC()
	out := Snapshot{
		Version:         Version,
		Variant:         variant,
		SampledAt:       &now,
		OS:              detectOS(opts.Etc),
		NTP:             ntpState(opts.Proc),
		MemoryTotal:     host.MemTotal,
		MemoryAvailable: host.MemAvailable,
		Coral:           coralPCIePresent(opts) || coralUSBPresent(opts),
		CoralUSB:        coralUSBPresent(opts),
		CoralPCIe:       coralPCIePresent(opts),
		OpenVINO:        openvinoPresent(opts, host),
		Hailo:           hailoPresent(opts),
		Nvidia:          nvidiaPresent(opts, host),
		CCTVTotal:       cctvTotal,
		CCTVFree:        cctvFree,
		DatabaseTotal:   dbTotal,
		DatabaseFree:    dbFree,
	}
	watchdogCfg := StorageWatchdogConfig{
		Enabled:             true,
		SafetyBufferPercent: DefaultStorageSafetyBufferPercent,
		RecordingsPath:      opts.CCTV,
		DBPath:              opts.DB,
	}
	watchdogStatus, _ := EnforceStorageSafety(watchdogCfg, cctvTotal, cctvFree, dbTotal, dbFree)
	out.StorageBufferPercent = watchdogStatus.SafetyBufferPercent
	out.StorageWatchdog = watchdogStatus.Enabled
	out.StorageOver85Percent = watchdogStatus.OverSafetyThreshold
	out.DatabaseCritical = watchdogStatus.DBCritical
	out.PrunedSegmentsCount = watchdogStatus.PrunedSegmentsCount
	if host.CPUAvailable {
		cpu := host.CPUPercent
		out.CPUPercent = &cpu
	}
	if len(host.GPUs) > 0 {
		out.GPUPresent = true
		out.GPUVendor = host.GPUs[0].Vendor
		out.GPUName = host.GPUs[0].Name
	}
	if out.Nvidia && !out.GPUPresent {
		out.GPUPresent = true
		if out.GPUVendor == "" {
			out.GPUVendor = "NVIDIA"
		}
	}
	return out
}

func coralPresent(opts Paths) bool {
	return coralPCIePresent(opts) || coralUSBPresent(opts)
}

func coralPCIePresent(opts Paths) bool {
	dev := opts.Dev
	if dev == "" {
		dev = "/dev"
	}
	if _, err := os.Stat(filepath.Join(dev, "apex_0")); err == nil {
		return true
	}
	sys := opts.Sys
	if sys == "" {
		sys = "/sys"
	}
	if _, err := os.Stat(filepath.Join(sys, "bus", "pci", "drivers", "apex")); err == nil {
		return true
	}
	return false
}

func coralUSBPresent(opts Paths) bool {
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

func hailoPresent(opts Paths) bool {
	dev := opts.Dev
	if dev == "" {
		dev = "/dev"
	}
	if _, err := os.Stat(filepath.Join(dev, "hailo0")); err == nil {
		return true
	}
	sys := opts.Sys
	if sys == "" {
		sys = "/sys"
	}
	if _, err := os.Stat(filepath.Join(sys, "class", "hailo_chardev")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(sys, "bus", "pci", "drivers", "hailo_pci")); err == nil {
		return true
	}
	pciDevices := filepath.Join(sys, "bus", "pci", "devices")
	if entries, err := os.ReadDir(pciDevices); err == nil {
		for _, e := range entries {
			vendor := strings.TrimSpace(readText(filepath.Join(pciDevices, e.Name(), "vendor")))
			if strings.EqualFold(vendor, "0x1e60") || strings.EqualFold(vendor, "1e60") {
				return true
			}
		}
	}
	return false
}

func nvidiaPresent(opts Paths, host hoststat.Snapshot) bool {
	for _, g := range host.GPUs {
		if strings.Contains(strings.ToLower(g.Vendor), "nvidia") || strings.Contains(strings.ToLower(g.Name), "nvidia") {
			return true
		}
	}
	dev := opts.Dev
	if dev == "" {
		dev = "/dev"
	}
	if _, err := os.Stat(filepath.Join(dev, "nvidia0")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dev, "nvidiactl")); err == nil {
		return true
	}
	proc := opts.Proc
	if proc == "" {
		proc = "/proc"
	}
	if _, err := os.Stat(filepath.Join(proc, "driver", "nvidia")); err == nil {
		return true
	}
	return false
}

func openvinoPresent(opts Paths, host hoststat.Snapshot) bool {
	for _, g := range host.GPUs {
		if strings.Contains(strings.ToLower(g.Vendor), "intel") {
			return true
		}
	}
	if host.OpenVINO {
		return true
	}
	proc := opts.Proc
	if proc == "" {
		proc = "/proc"
	}
	cpuinfo := readText(filepath.Join(proc, "cpuinfo"))
	if strings.Contains(cpuinfo, "GenuineIntel") && (strings.Contains(cpuinfo, "avx2") || strings.Contains(cpuinfo, "avx512")) {
		return true
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
	// #nosec G304 -- proc/sysfs path is sourced from local configuration, not remote input.
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
	return filesystemBytes(st.Blocks, st.Bsize), filesystemBytes(st.Bavail, st.Bsize)
}

func filesystemBytes(blocks uint64, blockSize int64) uint64 {
	if blockSize <= 0 {
		return 0
	}
	unsignedBlockSize := uint64(blockSize)
	if blocks > math.MaxUint64/unsignedBlockSize {
		return math.MaxUint64
	}
	return blocks * unsignedBlockSize
}

func readText(path string) string {
	// #nosec G304 -- proc/sysfs path is sourced from local configuration, not remote input.
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
