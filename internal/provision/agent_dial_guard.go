package provision

import (
	"context"
	"errors"
	"net"
	"strings"
)

// A stored agent address is chosen by an operator and then dialled by the server with the agent's
// bearer token, so an address can aim OpenVMS at things it should not reach. The guard refuses the
// destinations that are never an agent and keeps the token off the open internet.
var (
	errAgentAddressForbidden = errors.New("the agent address resolves to a destination OpenVMS does not connect to")
	errAgentPlainPublic      = errors.New("plain HTTP to a public address would send the agent token in clear; use https")
)

var carrierGradeNAT = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// checkAgentDialAddress decides whether OpenVMS may connect to ip for a scheme. Never: this host
// (loopback), link-local (the cloud metadata address lives there), unspecified, multicast and the
// broadcast address. Private ranges, including the 100.64/10 used by VPN overlays, take either
// scheme; a public address takes https only.
func checkAgentDialAddress(ip net.IP, scheme string) error {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	switch {
	case ip == nil, ip.IsLoopback(), ip.IsUnspecified(), ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip.IsInterfaceLocalMulticast(), ip.IsMulticast():
		return errAgentAddressForbidden
	case ip.Equal(net.IPv4bcast):
		return errAgentAddressForbidden
	case ip.IsPrivate(), carrierGradeNAT.Contains(ip):
		return nil
	case scheme == "https":
		return nil
	default:
		return errAgentPlainPublic
	}
}

// guardedDialContext resolves the host once, requires every answer to be allowed (one forbidden
// address next to a good one is how DNS rebinding hides), and dials an approved address directly, so
// a second lookup cannot return something else.
func guardedDialContext(scheme string, lookup func(context.Context, string) ([]net.IP, error), dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, errAgentAddressForbidden
		}
		var ips []net.IP
		if ip := net.ParseIP(host); ip != nil {
			ips = []net.IP{ip}
		} else if ips, err = lookup(ctx, strings.ToLower(host)); err != nil || len(ips) == 0 {
			return nil, errAgentAddressForbidden
		}
		for _, ip := range ips {
			if err := checkAgentDialAddress(ip, scheme); err != nil {
				return nil, err
			}
		}
		// Every answer is approved; try them in order, as the stock dialer does, so an unreachable first
		// address (an IPv6 answer on a host with no route) does not fail a name that has another.
		var lastErr error
		for _, ip := range ips {
			conn, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				break
			}
		}
		return nil, lastErr
	}
}

// systemLookup is the resolver the production transport uses.
func systemLookup(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP)
	}
	return out, nil
}
