package camproxy

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// echoCamera is a camera-side TCP service that upper-cases whatever it receives.
func echoCamera(t *testing.T) (addr string, accepted <-chan struct{}) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := make(chan struct{}, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			got <- struct{}{}
			go func() {
				defer c.Close()
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					_, _ = c.Write([]byte(strings.ToUpper(sc.Text()) + "\n"))
				}
			}()
		}
	}()
	return ln.Addr().String(), got
}

func openTunnel(t *testing.T, agent *httptest.Server, target string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()
	c, err := net.Dial("tcp", strings.TrimPrefix(agent.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	req := "GET " + Path + " HTTP/1.1\r\nHost: agent\r\nConnection: Upgrade\r\nUpgrade: " + TunnelProtocol + "\r\n" +
		HeaderTarget + ": " + target + "\r\nAuthorization: Bearer agent-secret\r\n\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(c)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return c, br, resp
}

func TestTunnelRelaysRawBytesBothWays(t *testing.T) {
	cam, accepted := echoCamera(t)
	agent := httptest.NewServer(newTestHandler(t, Config{}))
	t.Cleanup(agent.Close)

	c, br, resp := openTunnel(t, agent, "http://"+cam)
	if resp.StatusCode != http.StatusSwitchingProtocols || resp.Header.Get(HeaderProxy) != "1" {
		t.Fatalf("status %d headers %v", resp.StatusCode, resp.Header)
	}
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("camera never saw a connection")
	}
	if _, err := c.Write([]byte("hello\nsecond\n")); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"HELLO", "SECOND"} {
		line, err := br.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != want {
			t.Fatalf("got %q err %v, want %q", line, err, want)
		}
	}
}

func TestTunnelRefusesWhatTheHTTPPathRefuses(t *testing.T) {
	agent := httptest.NewServer(New(Config{BlockedPorts: []int{7419}, SelfAddrs: func() []netip.Addr { return nil }}))
	t.Cleanup(agent.Close)
	for name, target := range map[string]string{
		"loopback":    "http://127.0.0.1:80",
		"public":      "http://8.8.8.8:80",
		"ssh":         "http://10.0.0.5:22",
		"agent port":  "http://10.0.0.5:7419",
		"hostname":    "http://camera.local:80",
		"bad scheme":  "ws://10.0.0.5:80",
		"empty":       "",
		"with a path": "http://10.0.0.5:80/x",
	} {
		_, _, resp := openTunnel(t, agent, target)
		if resp.StatusCode == http.StatusSwitchingProtocols || resp.Header.Get(HeaderProxyError) == "" {
			t.Errorf("%s: tunnel to %q must be refused, got %d", name, target, resp.StatusCode)
		}
	}
}

func TestTunnelUnreachableCameraIsBadGateway(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close() // nothing listens there now
	agent := httptest.NewServer(newTestHandler(t, Config{}))
	t.Cleanup(agent.Close)
	_, _, resp := openTunnel(t, agent, "http://"+addr)
	if resp.StatusCode != http.StatusBadGateway || resp.Header.Get(HeaderProxyError) != "camera_unreachable" {
		t.Fatalf("got %d %q", resp.StatusCode, resp.Header.Get(HeaderProxyError))
	}
}
