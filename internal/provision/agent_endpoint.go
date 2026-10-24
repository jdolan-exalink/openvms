package provision

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const maxAgentHostLen = 253

// AgentEndpoint is where OpenVMS reaches an edge agent's REST API: a LAN address, a VPN address or a
// name published through a tunnel. Scheme is http or https, Host an IPv4 literal or a DNS name, and
// the port is explicit (the scheme's default when the URL has none). There is no path: the agent
// answers at the root of its authority.
type AgentEndpoint struct {
	Scheme string
	Host   string
	Port   int
}

// ParseAgentEndpoint reads "http(s)://host[:port]". User information, a path other than "/", a
// query, a fragment and IPv6 literals are refused: the value is stored and later dialled with a
// bearer token, so it has to be exactly an authority.
func ParseAgentEndpoint(raw string) (AgentEndpoint, error) {
	invalid := errors.New("the agent address must look like https://host[:port] or http://host:port")
	if raw == "" || len(raw) > maxAgentHostLen+16 || strings.ContainsAny(raw, " \t\r\n\\") {
		return AgentEndpoint{}, invalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return AgentEndpoint{}, invalid
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return AgentEndpoint{}, invalid
	}
	host := strings.ToLower(u.Hostname())
	if !validAgentHost(host) {
		return AgentEndpoint{}, invalid
	}
	port := 80
	if scheme == "https" {
		port = 443
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return AgentEndpoint{}, invalid
		}
		port = n
	} else if strings.HasSuffix(u.Host, ":") {
		return AgentEndpoint{}, invalid
	}
	return AgentEndpoint{Scheme: scheme, Host: host, Port: port}, nil
}

// AgentEndpointFromRow builds the endpoint of a registered agent: the stored base URL when there is
// one, otherwise the legacy host and port (plain HTTP on the LAN).
func AgentEndpointFromRow(host string, port int32, baseURL string) (AgentEndpoint, error) {
	if baseURL != "" {
		return ParseAgentEndpoint(baseURL)
	}
	if !validAgentHost(host) || port < 1 || port > 65535 {
		return AgentEndpoint{}, errors.New("the registered agent address is invalid")
	}
	return AgentEndpoint{Scheme: "http", Host: strings.ToLower(host), Port: int(port)}, nil
}

// BaseURL is the scheme, host and port with no trailing slash.
func (e AgentEndpoint) BaseURL() string {
	return e.Scheme + "://" + net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}

// validAgentHost accepts an IPv4 literal or an RFC 1123 host name; anything that looks like an IPv4
// address but is not one (256.1.1.1, 1.2.3) is refused instead of being taken for a name.
func validAgentHost(host string) bool {
	if host == "" || len(host) > maxAgentHostLen {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.To4() != nil && !strings.Contains(host, ":")
	}
	labels := strings.Split(host, ".")
	allNumeric := true
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		numeric := true
		for _, r := range l {
			switch {
			case r >= '0' && r <= '9':
			case r >= 'a' && r <= 'z', r == '-':
				numeric = false
			default:
				return false
			}
		}
		allNumeric = allNumeric && numeric
	}
	return !allNumeric
}
