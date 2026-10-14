package agent

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverInterfaces(t *testing.T) {
	ifaces, err := DiscoverInterfaces()
	if err != nil {
		t.Fatalf("DiscoverInterfaces returned error: %v", err)
	}

	for _, iface := range ifaces {
		if iface.Name == "" {
			t.Errorf("expected non-empty interface name")
		}
		if iface.IPAddress == "" {
			t.Errorf("expected non-empty IP address for interface %s", iface.Name)
		}
	}
}

func TestParseDefaultRoute(t *testing.T) {
	d := t.TempDir()
	netDir := filepath.Join(d, "net")
	if err := os.MkdirAll(netDir, 0o750); err != nil {
		t.Fatal(err)
	}
	routeContent := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"eth0\t00000000\t0101A8C0\t0003\t0\t0\t0\t00000000\t0\t0\t0\n" +
		"eth1\t0002A8C0\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n"
	if err := os.WriteFile(filepath.Join(netDir, "route"), []byte(routeContent), 0o600); err != nil {
		t.Fatal(err)
	}

	iface, gw, err := parseDefaultRoute(d)
	if err != nil {
		t.Fatalf("unexpected error parsing route: %v", err)
	}
	if iface != "eth0" {
		t.Fatalf("got default interface %q, want eth0", iface)
	}
	if gw != "192.168.1.1" {
		t.Fatalf("got default gateway %q, want 192.168.1.1", gw)
	}
}

func TestExtractCameraIPsFromFrigateConfig(t *testing.T) {
	d := t.TempDir()
	configPath := filepath.Join(d, "config.yml")
	// #nosec G101 -- mock stream url for unit tests
	configContent := `
cameras:
  front:
    ffmpeg:
      inputs:
        - path: rtsp://operator:streamkey@192.168.2.50:554/live
  back:
    ffmpeg:
      inputs:
        - path: http://192.168.2.51:80/stream
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	ips := extractCameraIPsFromFrigateConfig(configPath)
	if len(ips) != 2 {
		t.Fatalf("expected 2 camera IPs, got %d", len(ips))
	}
	expected := []string{"192.168.2.50", "192.168.2.51"}
	for i, exp := range expected {
		if ips[i].String() != exp {
			t.Errorf("got IP %s, want %s", ips[i], exp)
		}
	}
}

func TestCalculateSubnet(t *testing.T) {
	_, ipNet, err := net.ParseCIDR("192.168.1.150/24")
	if err != nil {
		t.Fatal(err)
	}
	netmask, subnet := calculateSubnet(ipNet)
	if netmask != "255.255.255.0" {
		t.Errorf("got netmask %q, want 255.255.255.0", netmask)
	}
	if subnet != "192.168.1.0/24" {
		t.Errorf("got subnet %q, want 192.168.1.0/24", subnet)
	}
}

func TestClassifyInterfaceRole(t *testing.T) {
	camIP := net.ParseIP("192.168.2.50")
	cameraIPs := []net.IP{camIP}

	// Interface matches camera subnet -> cameras
	if role := classifyInterfaceRole("192.168.2.0/24", false, true, cameraIPs); role != "cameras" {
		t.Errorf("got role %q, want cameras", role)
	}

	// Interface has default gateway in dual-NIC setup -> monitoring
	if role := classifyInterfaceRole("10.0.0.0/24", true, true, cameraIPs); role != "monitoring" {
		t.Errorf("got role %q, want monitoring", role)
	}

	// Interface in dual-NIC without default gateway and without explicit camera match -> cameras
	if role := classifyInterfaceRole("172.16.0.0/24", false, true, nil); role != "cameras" {
		t.Errorf("got role %q, want cameras", role)
	}

	// Single interface -> monitoring
	if role := classifyInterfaceRole("192.168.1.0/24", true, false, nil); role != "monitoring" {
		t.Errorf("got role %q, want monitoring", role)
	}
}

func TestDiscoverInterfacesHermetic(t *testing.T) {
	d := t.TempDir()
	netDir := filepath.Join(d, "proc", "net")
	if err := os.MkdirAll(netDir, 0o750); err != nil {
		t.Fatal(err)
	}
	routeContent := "Iface\tDestination\tGateway\tFlags\n" +
		"eth0\t00000000\t0101A8C0\t0003\n"
	if err := os.WriteFile(filepath.Join(netDir, "route"), []byte(routeContent), 0o600); err != nil {
		t.Fatal(err)
	}

	dbDir := filepath.Join(d, "frigate")
	if err := os.MkdirAll(dbDir, 0o750); err != nil {
		t.Fatal(err)
	}
	configContent := "cameras:\n  cam1:\n    ffmpeg:\n      inputs:\n        - path: rtsp://192.168.2.10:554/h264\n"
	if err := os.WriteFile(filepath.Join(dbDir, "config.yml"), []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	macEth0, _ := net.ParseMAC("00:11:22:33:44:01")
	macEth1, _ := net.ParseMAC("00:11:22:33:44:02")

	fakeIfaces := []net.Interface{
		{Name: "eth0", Flags: net.FlagUp | net.FlagBroadcast, HardwareAddr: macEth0},
		{Name: "eth1", Flags: net.FlagUp | net.FlagBroadcast, HardwareAddr: macEth1},
		{Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
		{Name: "docker0", Flags: net.FlagUp | net.FlagBroadcast},
		{Name: "br-2996d6b23960", Flags: net.FlagUp | net.FlagBroadcast},
		{Name: "br-lan", Flags: net.FlagUp | net.FlagBroadcast},
	}

	_, ipNetEth0, _ := net.ParseCIDR("192.168.1.100/24")
	_, ipNetEth1, _ := net.ParseCIDR("192.168.2.100/24")

	fakeAddrs := func(i net.Interface) ([]net.Addr, error) {
		switch i.Name {
		case "eth0":
			return []net.Addr{ipNetEth0}, nil
		case "eth1":
			return []net.Addr{ipNetEth1}, nil
		default:
			return nil, nil
		}
	}

	paths := Paths{Proc: filepath.Join(d, "proc"), DB: dbDir}
	results, err := discoverInterfaces(paths, func() ([]net.Interface, error) { return fakeIfaces, nil }, fakeAddrs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 discovered interfaces, got %d", len(results))
	}

	// eth0 is default (monitoring)
	if results[0].Name != "eth0" || !results[0].IsDefault || results[0].Role != "monitoring" || results[0].Gateway != "192.168.1.1" {
		t.Errorf("eth0 mismatch: %+v", results[0])
	}

	// eth1 matches camera IP (cameras)
	if results[1].Name != "eth1" || results[1].IsDefault || results[1].Role != "cameras" || results[1].Subnet != "192.168.2.0/24" {
		t.Errorf("eth1 mismatch: %+v", results[1])
	}
}

func TestIsLANInterfaceFilters(t *testing.T) {
	// Excluded
	for _, name := range []string{"lo", "lo:0", "docker0", "br-4b3d3e0117cd", "veth1234", "virbr0", "tun0", "tap0"} {
		if isLANInterface(name) {
			t.Errorf("expected %q to be excluded", name)
		}
	}
	// Included
	for _, name := range []string{"eth0", "eth1", "enp3s0", "ens18", "eno1", "wlan0", "bond0", "br0", "vmbr0", "br-lan"} {
		if !isLANInterface(name) {
			t.Errorf("expected %q to be included", name)
		}
	}
}
