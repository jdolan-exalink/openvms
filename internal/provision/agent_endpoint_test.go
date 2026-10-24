package provision

import "testing"

func TestParseAgentEndpointAcceptsLANAndPublishedAddresses(t *testing.T) {
	for _, tc := range []struct {
		raw, scheme, host string
		port              int
		base              string
	}{
		{"http://10.0.15.2:7419", "http", "10.0.15.2", 7419, "http://10.0.15.2:7419"},
		{"https://agent.example.com", "https", "agent.example.com", 443, "https://agent.example.com:443"},
		{"https://agent.example.com:8443/", "https", "agent.example.com", 8443, "https://agent.example.com:8443"},
		{"HTTPS://Agent.Example.COM", "https", "agent.example.com", 443, "https://agent.example.com:443"},
		{"http://frigate-1.lan:7419", "http", "frigate-1.lan", 7419, "http://frigate-1.lan:7419"},
		{"http://192.0.2.9", "http", "192.0.2.9", 80, "http://192.0.2.9:80"},
	} {
		got, err := ParseAgentEndpoint(tc.raw)
		if err != nil {
			t.Fatalf("%q: %v", tc.raw, err)
		}
		if got.Scheme != tc.scheme || got.Host != tc.host || got.Port != tc.port || got.BaseURL() != tc.base {
			t.Errorf("%q = %+v (%s), want %s %s %d %s", tc.raw, got, got.BaseURL(), tc.scheme, tc.host, tc.port, tc.base)
		}
	}
}

func TestParseAgentEndpointRefusesAnythingThatIsNotAPlainAuthority(t *testing.T) {
	for _, raw := range []string{
		"", "agent.example.com", "ftp://agent.example.com", "https://", "https://user:pw@agent.example.com",
		"https://agent.example.com/v1", "https://agent.example.com?x=1", "https://agent.example.com#f",
		"https://agent.example.com:0", "https://agent.example.com:70000", "https://agent.example.com:abc",
		"https://[::1]:7419", "https://-bad.example.com", "https://bad_name.example.com", "https://a..b.example.com",
		"https://256.1.1.1", "https://1.2.3", "https://agent.example.com\n", "https://agent .example.com",
		"https://" + string(make([]byte, 300)),
	} {
		if got, err := ParseAgentEndpoint(raw); err == nil {
			t.Errorf("%q must be refused, got %+v", raw, got)
		}
	}
}

func TestAgentEndpointFromRowKeepsTheLegacyHostAndPort(t *testing.T) {
	e, err := AgentEndpointFromRow("10.0.15.2", 7419, "")
	if err != nil || e.BaseURL() != "http://10.0.15.2:7419" {
		t.Fatalf("legacy row = %+v, %v", e, err)
	}
	e, err = AgentEndpointFromRow("10.0.15.2", 7419, "https://agent.example.com")
	if err != nil || e.BaseURL() != "https://agent.example.com:443" {
		t.Fatalf("a base URL wins over host and port: %+v, %v", e, err)
	}
	if _, err := AgentEndpointFromRow("not an address", 7419, ""); err == nil {
		t.Fatal("a legacy row with an invalid host must be refused")
	}
}
