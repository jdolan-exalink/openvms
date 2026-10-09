package agent

import (
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
