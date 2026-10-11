package control

import "testing"

func TestParseHostPortRejectsPortsOutsideTCPRange(t *testing.T) {
	for _, raw := range []string{"http://camera.local:0", "http://camera.local:65536", "http://camera.local:2147483648"} {
		t.Run(raw, func(t *testing.T) {
			host, port := parseHostPort(raw)
			if host != "camera.local" || port != 0 {
				t.Fatalf("parseHostPort(%q) = (%q, %d), want host and invalid-port sentinel", raw, host, port)
			}
		})
	}
}
