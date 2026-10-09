package agent

import (
	"net"
	"strings"
)

// InterfaceInfo represents a physical or virtual network interface on the node.
type InterfaceInfo struct {
	Name      string `json:"name"`
	IPAddress string `json:"ip_address"`
	MAC       string `json:"mac_address"`
	IsDefault bool   `json:"is_default"`
}

// DiscoverInterfaces scans the node's local network adapters for IPv4 addresses.
func DiscoverInterfaces() ([]InterfaceInfo, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var out []InterfaceInfo
	foundDefault := false

	for _, iface := range ifaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() {
				continue
			}

			ipv4 := ip.To4()
			if ipv4 == nil {
				continue // For now, index primary IPv4 addresses
			}

			isDef := false
			if !foundDefault && (strings.HasPrefix(iface.Name, "eth") || strings.HasPrefix(iface.Name, "en") || strings.HasPrefix(iface.Name, "wl")) {
				isDef = true
				foundDefault = true
			}

			out = append(out, InterfaceInfo{
				Name:      iface.Name,
				IPAddress: ipv4.String(),
				MAC:       iface.HardwareAddr.String(),
				IsDefault: isDef,
			})
		}
	}

	if !foundDefault && len(out) > 0 {
		out[0].IsDefault = true
	}

	return out, nil
}
