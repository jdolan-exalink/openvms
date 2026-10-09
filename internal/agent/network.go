package agent

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxNetworkProcBytes = 64 * 1024

var errNetworkProcTooLarge = errors.New("network counters exceed size limit")

// NetworkInterface reports rates for one non-loopback interface.
type NetworkInterface struct {
	Name             string  `json:"name"`
	RXBytesPerSecond float64 `json:"rx_bytes_per_second"`
	TXBytesPerSecond float64 `json:"tx_bytes_per_second"`
}

// NetworkSnapshot is absent until a valid counter interval is observed.
type NetworkSnapshot struct {
	Interfaces []NetworkInterface `json:"interfaces,omitempty"`
	SampledAt  *time.Time         `json:"sampled_at,omitempty"`
}

type networkCounters struct{ rx, tx uint64 }
type networkBaseline struct {
	counters map[string]networkCounters
	at       time.Time
}

// NetworkSampler serializes bounded counter reads and derives rates from successful
// samples using the monotonic component of time.Time.
type NetworkSampler struct {
	mu   sync.Mutex
	read func() ([]byte, error)
	now  func() time.Time
	last *networkBaseline
}

func newNetworkSampler(read func() ([]byte, error), now func() time.Time) *NetworkSampler {
	return &NetworkSampler{read: read, now: now}
}

func newProcNetworkSampler(proc string) *NetworkSampler {
	return newNetworkSampler(func() ([]byte, error) {
		f, err := os.Open(filepath.Join(proc, "net/dev"))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, maxNetworkProcBytes+1))
		if err != nil {
			return nil, err
		}
		if len(b) > maxNetworkProcBytes {
			return nil, errNetworkProcTooLarge
		}
		return b, nil
	}, time.Now)
}

func (s *NetworkSampler) Sample() NetworkSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.read == nil || s.now == nil {
		return NetworkSnapshot{}
	}
	b, err := s.read()
	if err != nil {
		s.last = nil
		return NetworkSnapshot{}
	}
	counters, err := parseNetworkCounters(b)
	if err != nil {
		s.last = nil
		return NetworkSnapshot{}
	}
	now := s.now()
	previous := s.last
	if previous == nil {
		s.last = &networkBaseline{counters: counters, at: now}
		return NetworkSnapshot{}
	}
	elapsed := now.Sub(previous.at)
	if elapsed <= 0 {
		s.last = &networkBaseline{counters: counters, at: now}
		return NetworkSnapshot{}
	}
	if elapsed < time.Second {
		return NetworkSnapshot{}
	}
	s.last = &networkBaseline{counters: counters, at: now}
	interfaces := make([]NetworkInterface, 0, len(counters))
	seconds := elapsed.Seconds()
	for name, value := range counters {
		prior, ok := previous.counters[name]
		if !ok || value.rx < prior.rx || value.tx < prior.tx {
			continue
		}
		interfaces = append(interfaces, NetworkInterface{
			Name: name, RXBytesPerSecond: float64(value.rx-prior.rx) / seconds,
			TXBytesPerSecond: float64(value.tx-prior.tx) / seconds,
		})
	}
	if len(interfaces) == 0 {
		return NetworkSnapshot{}
	}
	sort.Slice(interfaces, func(i, j int) bool { return interfaces[i].Name < interfaces[j].Name })
	sampledAt := now.UTC()
	return NetworkSnapshot{Interfaces: interfaces, SampledAt: &sampledAt}
}

func parseNetworkCounters(b []byte) (map[string]networkCounters, error) {
	if len(b) == 0 || len(b) > maxNetworkProcBytes {
		return nil, errors.New("invalid network counter input size")
	}
	scanner := bufio.NewScanner(bytes.NewReader(b))
	scanner.Buffer(make([]byte, 1024), maxNetworkProcBytes)
	lines := 0
	out := make(map[string]networkCounters)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lines++
		if line == "" || strings.HasPrefix(line, "Inter-") || strings.HasPrefix(line, "face |") {
			continue
		}
		colon := strings.LastIndexByte(line, ':')
		if colon < 1 {
			return nil, errors.New("malformed network counter line")
		}
		name := strings.TrimSpace(line[:colon])
		if name == "" || len(name) > 64 || strings.ContainsAny(name, " \t\r\n") {
			return nil, errors.New("invalid network interface name")
		}
		fields := strings.Fields(line[colon+1:])
		if len(fields) != 16 {
			return nil, errors.New("malformed network counter fields")
		}
		values := make([]uint64, len(fields))
		for i, field := range fields {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return nil, errors.New("invalid network counter value")
			}
			values[i] = value
		}
		if !isLANInterface(name) {
			continue
		}
		if _, exists := out[name]; exists {
			return nil, errors.New("duplicate network interface")
		}
		if len(out) >= 256 {
			return nil, errors.New("too many network interfaces")
		}
		out[name] = networkCounters{rx: values[0], tx: values[8]}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if lines < 2 || len(out) == 0 {
		return nil, errors.New("no network counters found")
	}
	return out, nil
}

// isLANInterface returns true only for physical or LAN interfaces,
// excluding loopback, Docker bridges, veth pairs, virtual bridges, and tunnels.
func isLANInterface(name string) bool {
	if name == "lo" || strings.HasPrefix(name, "lo:") {
		return false
	}
	for _, prefix := range []string{
		"docker", "br-", "veth", "virbr", "dummy", "tun", "tap",
		"flannel", "cni", "kube", "vnet", "sit", "ip6tnl",
	} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}

