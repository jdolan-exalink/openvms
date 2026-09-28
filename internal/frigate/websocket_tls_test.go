package frigate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jdolan-exalink/openvms/internal/frigatemock"
)

// TestWebSocketOverHTTP2CapableTLS reproduces a Frigate behind an HTTPS proxy that
// negotiates HTTP/2: the websocket dial must stay on HTTP/1.1 even after the shared
// HTTP transport has advertised h2 on the same TLS config.
func TestWebSocketOverHTTP2CapableTLS(t *testing.T) {
	cams, _ := frigatemock.ParseCameras("plaza:centro")
	mock := &frigatemock.Server{Version: "0.18.0", Cameras: cams, Store: frigatemock.NewStore(10), User: "vms", Password: "pw", RequireAuth: true, StartedAt: time.Now()}
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/live/mse/api/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = c.WriteMessage(websocket.TextMessage, []byte("ok"))
		_ = c.Close()
	})
	mux.Handle("/", mock.Handler())
	ts := httptest.NewUnstartedServer(mux)
	ts.EnableHTTP2 = true
	ts.StartTLS()
	t.Cleanup(ts.Close)

	ctx := context.Background()
	a, err := Connect(ctx, ConnInfo{BaseURL: ts.URL, Username: "vms", Password: "pw", TLSSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	target, header, tlsCfg, err := a.Media().WebSocket(ctx, "/live/mse/api/ws", url.Values{"src": {"plaza"}})
	if err != nil {
		t.Fatal(err)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second, TLSClientConfig: tlsCfg}
	conn, resp, err := dialer.DialContext(ctx, target, header)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("websocket dial over h2-capable TLS: %v", err)
	}
	defer conn.Close()
	_, msg, err := conn.ReadMessage()
	if err != nil || string(msg) != "ok" {
		t.Fatalf("want ok, got %q (%v)", msg, err)
	}
}
