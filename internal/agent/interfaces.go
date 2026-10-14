package agent

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// InterfaceInfo represents a physical or virtual network interface on the node.
type InterfaceInfo struct {
	Name      string `json:"name"`
	IPAddress string `json:"ip_address"`
	Netmask   string `json:"netmask,omitempty"`
	Subnet    string `json:"subnet,omitempty"`
	MAC       string `json:"mac_address"`
	Gateway   string `json:"gateway,omitempty"`
	IsDefault bool   `json:"is_default"`
	Role      string `json:"role,omitempty"` // "cameras" | "monitoring" | "lan"
}

// DiscoverInterfaces scans the node's local network adapters using standard paths.
func DiscoverInterfaces() ([]InterfaceInfo, error) {
	return DiscoverInterfacesWithPaths(Paths{Proc: "/proc", DB: "/opt/frigate/config"})
}

// DiscoverInterfacesWithPaths scans local network adapters using configurable paths.
func DiscoverInterfacesWithPaths(paths Paths) ([]InterfaceInfo, error) {
	return discoverInterfaces(paths, net.Interfaces, func(i net.Interface) ([]net.Addr, error) {
		return i.Addrs()
	})
}

func discoverInterfaces(
	paths Paths,
	netIfaces func() ([]net.Interface, error),
	ifaceAddrs func(net.Interface) ([]net.Addr, error),
) ([]InterfaceInfo, error) {
	if netIfaces == nil {
		netIfaces = net.Interfaces
	}
	if ifaceAddrs == nil {
		ifaceAddrs = func(i net.Interface) ([]net.Addr, error) { return i.Addrs() }
	}

	ifaces, err := netIfaces()
	if err != nil {
		return nil, err
	}

	proc := paths.Proc
	if proc == "" {
		proc = "/proc"
	}
	defaultIface, defaultGateway, _ := parseDefaultRoute(proc)

	var configPath string
	if paths.DB != "" {
		configPath = filepath.Join(paths.DB, "config.yml")
	}
	cameraIPs := extractCameraIPsFromFrigateConfig(configPath)

	type candidate struct {
		iface net.Interface
		ipv4  net.IP
		ipNet *net.IPNet
	}
	var candidates []candidate

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		if !isLANInterface(iface.Name) {
			continue
		}
		addrs, err := ifaceAddrs(iface)
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ipNet *net.IPNet
			switch v := addr.(type) {
			case *net.IPNet:
				ipNet = v
			}
			if ipNet == nil || ipNet.IP == nil || ipNet.IP.IsLoopback() {
				continue
			}
			ipv4 := ipNet.IP.To4()
			if ipv4 == nil {
				continue
			}
			candidates = append(candidates, candidate{iface: iface, ipv4: ipv4, ipNet: ipNet})
			break
		}
	}

	hasMultiple := len(candidates) > 1
	foundDefault := false
	var out []InterfaceInfo

	for _, c := range candidates {
		isDef := false
		var gw string
		if defaultIface != "" && c.iface.Name == defaultIface {
			isDef = true
			gw = defaultGateway
			foundDefault = true
		}
		netmask, subnet := calculateSubnet(c.ipNet)
		role := classifyInterfaceRole(subnet, isDef, hasMultiple, cameraIPs)

		out = append(out, InterfaceInfo{
			Name:      c.iface.Name,
			IPAddress: c.ipv4.String(),
			Netmask:   netmask,
			Subnet:    subnet,
			MAC:       c.iface.HardwareAddr.String(),
			Gateway:   gw,
			IsDefault: isDef,
			Role:      role,
		})
	}

	if !foundDefault && len(out) > 0 {
		out[0].IsDefault = true
		if out[0].Role == "" {
			out[0].Role = "monitoring"
		}
	}

	return out, nil
}

func parseDefaultRoute(proc string) (defaultIface string, defaultGateway string, err error) {
	if proc == "" {
		proc = "/proc"
	}
	// #nosec G304 -- proc path is sourced from local agent configuration
	f, err := os.Open(filepath.Join(proc, "net/route"))
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		iface := fields[0]
		dest := fields[1]
		gwHex := fields[2]
		flagsStr := fields[3]
		flags, err := strconv.ParseUint(flagsStr, 16, 32)
		if err != nil {
			continue
		}
		if dest == "00000000" && (flags&0x0002) != 0 {
			gwVal, err := strconv.ParseUint(gwHex, 16, 32)
			if err == nil {
				val := uint32(gwVal)
				gwIP := net.IPv4(byte(val), byte(val>>8), byte(val>>16), byte(val>>24))
				return iface, gwIP.String(), nil
			}
			return iface, "", nil
		}
	}
	return "", "", nil
}

func extractCameraIPsFromFrigateConfig(configPath string) []net.IP {
	if configPath == "" {
		return nil
	}
	// #nosec G304 -- config path is sourced from local agent configuration
	b, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	re := regexp.MustCompile(`(?:rtsp|rtsps|http|https)://(?:[^@/\s]+@)?(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)
	matches := re.FindAllSubmatch(b, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var out []net.IP
	for _, m := range matches {
		ipStr := string(m[1])
		if seen[ipStr] {
			continue
		}
		seen[ipStr] = true
		ip := net.ParseIP(ipStr)
		if ip != nil && ip.To4() != nil {
			out = append(out, ip.To4())
		}
	}
	return out
}

func calculateSubnet(ipNet *net.IPNet) (netmask, subnet string) {
	if ipNet == nil {
		return "", ""
	}
	ipv4 := ipNet.IP.To4()
	if ipv4 == nil {
		return "", ""
	}
	mask := ipNet.Mask
	if len(mask) != 4 {
		return "", ""
	}
	netmask = net.IP(mask).String()
	networkIP := ipv4.Mask(mask)
	ones, _ := mask.Size()
	subnet = fmt.Sprintf("%s/%d", networkIP.String(), ones)
	return netmask, subnet
}

func classifyInterfaceRole(subnetStr string, isDefault bool, multipleInterfaces bool, cameraIPs []net.IP) string {
	if subnetStr != "" {
		_, ipNet, err := net.ParseCIDR(subnetStr)
		if err == nil && ipNet != nil {
			for _, camIP := range cameraIPs {
				if ipNet.Contains(camIP) {
					return "cameras"
				}
			}
		}
	}
	if multipleInterfaces {
		if isDefault {
			return "monitoring"
		}
		return "cameras"
	}
	return "monitoring"
}
