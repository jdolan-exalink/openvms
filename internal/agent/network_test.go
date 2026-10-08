package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const networkFixture = `Inter-| Receive | Transmit
 face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed
  eth0: 1000 1 0 0 0 0 0 0 2000 1 0 0 0 0 0 0
 lo: 9000 1 0 0 0 0 0 0 9000 1 0 0 0 0 0 0
`

func TestNetworkSamplerFirstAndMinimumIntervalAreUnavailable(t *testing.T) {
	now := time.Unix(100, 0)
	body := networkFixture
	s := newNetworkSampler(func() ([]byte, error) { return []byte(body), nil }, func() time.Time { return now })
	if got := s.Sample(); len(got.Interfaces) != 0 || got.SampledAt != nil {
		t.Fatalf("first counter sample must be unavailable: %#v", got)
	}
	now = now.Add(500 * time.Millisecond)
	body = strings.ReplaceAll(body, "1000 1", "1500 1")
	if got := s.Sample(); len(got.Interfaces) != 0 {
		t.Fatalf("sub-second interval must not produce rates: %#v", got)
	}
	now = now.Add(1500 * time.Millisecond)
	body = strings.ReplaceAll(body, "1500 1", "2500 1")
	got := s.Sample()
	if len(got.Interfaces) != 1 || got.Interfaces[0].Name != "eth0" || got.Interfaces[0].RXBytesPerSecond != 750 || got.Interfaces[0].TXBytesPerSecond != 0 {
		t.Fatalf("unexpected rate sample: %#v", got)
	}
	if got.SampledAt == nil || !got.SampledAt.Equal(now) {
		t.Fatalf("sample timestamp missing: %#v", got.SampledAt)
	}
}

func TestParseNetworkCountersRejectsMalformedAndOversizedInput(t *testing.T) {
	if _, err := parseNetworkCounters([]byte(networkFixture)); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"", "eth0: 1 2", strings.Repeat("x", maxNetworkProcBytes+1), strings.Replace(networkFixture, "1000 1", "-1 1", 1)} {
		if _, err := parseNetworkCounters([]byte(input)); err == nil {
			t.Fatalf("expected malformed input to fail: %q", input[:min(len(input), 30)])
		}
	}
}

func TestNetworkSamplerResetsOnCounterDecreaseAndReadError(t *testing.T) {
	now := time.Unix(100, 0)
	body := networkFixture
	readErr := error(nil)
	s := newNetworkSampler(func() ([]byte, error) { return []byte(body), readErr }, func() time.Time { return now })
	s.Sample()
	now = now.Add(time.Second)
	body = strings.ReplaceAll(body, "1000 1", "2000 1")
	if got := s.Sample(); len(got.Interfaces) != 1 {
		t.Fatalf("expected valid baseline rate: %#v", got)
	}
	now = now.Add(time.Second)
	body = strings.ReplaceAll(body, "2000 1", "10 1")
	if got := s.Sample(); len(got.Interfaces) != 0 || got.SampledAt != nil {
		t.Fatalf("counter reset must be unavailable: %#v", got)
	}
	now = now.Add(time.Second)
	readErr = errors.New("read failed")
	if got := s.Sample(); len(got.Interfaces) != 0 || got.SampledAt != nil {
		t.Fatalf("read failure must not invent zero rates: %#v", got)
	}
}

func TestNetworkSamplerDoesNotReportMissingOrNewInterfacesUntilAValidInterval(t *testing.T) {
	now := time.Unix(100, 0)
	body := networkFixture + " wlan0: 50 1 0 0 0 0 0 0 75 1 0 0 0 0 0 0\n"
	s := newNetworkSampler(func() ([]byte, error) { return []byte(body), nil }, func() time.Time { return now })
	s.Sample()
	now = now.Add(time.Second)
	body = strings.ReplaceAll(networkFixture, "1000 1", "1100 1")
	got := s.Sample()
	if len(got.Interfaces) != 1 || got.Interfaces[0].Name != "eth0" {
		t.Fatalf("disappeared interface must not be emitted: %#v", got)
	}
	now = now.Add(time.Second)
	body += " wlan0: 150 1 0 0 0 0 0 0 175 1 0 0 0 0 0 0\n"
	got = s.Sample()
	if len(got.Interfaces) != 1 || got.Interfaces[0].Name != "eth0" {
		t.Fatalf("newly appeared interface must wait for a valid interval: %#v", got)
	}
}

func TestNetworkSamplerSerializesConcurrentSamples(t *testing.T) {
	var reads atomic.Int32
	now := time.Unix(100, 0)
	s := newNetworkSampler(func() ([]byte, error) {
		reads.Add(1)
		return []byte(networkFixture), nil
	}, func() time.Time { return now })
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := s.Sample(); len(got.Interfaces) != 0 {
				t.Errorf("same-time concurrent reads must not create an interval: %#v", got)
			}
		}()
	}
	wg.Wait()
	if reads.Load() != 16 {
		t.Fatalf("expected each serialized request to read once, got %d", reads.Load())
	}
}

func TestProcNetworkReaderBoundsFileSize(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "net"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "net/dev"), []byte(strings.Repeat("x", maxNetworkProcBytes+100)), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := newProcNetworkSampler(root).Sample(); len(got.Interfaces) != 0 || got.SampledAt != nil {
		t.Fatalf("oversized proc file must fail closed: %#v", got)
	}
}

func TestParseNetworkCountersFiltersVirtualAndDockerInterfaces(t *testing.T) {
	proc := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:    1951      20    0    0    0     0          0         0     1951      20    0    0    0     0       0          0
  eth0: 995778189 2152565    0 457590    0     0          0         0 12002960   28946    0    0    0     0       0          0
docker0:       0       0    0    0    0     0          0         0        0       0    0    2    0     0       0          0
br-4b3d3e0117cd: 9413270   14173    0    0    0     0          0         0  3496481   25959    0    2    0     0       0          0
veth15fbffe: 3750452    4114    0    0    0     0          0         0  3812908    5054    0    0    0     0       0          0
veth3e023d4: 13421822   19190    0    0    0     0          0         0  7249059   30102    0    0    0     0       0          0
virbr0:       0       0    0    0    0     0          0         0        0       0    0    0    0     0       0          0
  enp3s0: 1000 1 0 0 0 0 0 0 2000 1 0 0 0 0 0 0
`
	counters, err := parseNetworkCounters([]byte(proc))
	if err != nil {
		t.Fatalf("unexpected error parsing counters: %v", err)
	}
	if len(counters) != 2 {
		t.Fatalf("expected exactly 2 LAN interfaces (eth0, enp3s0), got %d: %#v", len(counters), counters)
	}
	if _, ok := counters["eth0"]; !ok {
		t.Errorf("expected eth0 to be present")
	}
	if _, ok := counters["enp3s0"]; !ok {
		t.Errorf("expected enp3s0 to be present")
	}
	for _, excluded := range []string{"lo", "docker0", "br-4b3d3e0117cd", "veth15fbffe", "veth3e023d4", "virbr0"} {
		if _, ok := counters[excluded]; ok {
			t.Errorf("interface %s should have been excluded", excluded)
		}
	}
}

