package relay

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRelay_AuthorizeAndValidate(t *testing.T) {
	srv := NewServer(Config{TokenTTL: 50 * time.Millisecond})

	sess, err := srv.AuthorizeSession("http://node-1.lan:5000", "cam-1", "desktop-1")
	if err != nil {
		t.Fatalf("AuthorizeSession failed: %v", err)
	}

	if sess.Token == "" {
		t.Errorf("expected non-empty token")
	}

	got, err := srv.ValidateToken(sess.Token)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if got.NodeAddress != "http://node-1.lan:5000" {
		t.Errorf("expected node address, got %s", got.NodeAddress)
	}

	// Wait for token expiration
	time.Sleep(70 * time.Millisecond)
	_, err = srv.ValidateToken(sess.Token)
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestRelay_StreamProxy(t *testing.T) {
	// 1. Mock upstream node server
	nodeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mock_video_stream_bytes"))
	}))
	defer nodeSrv.Close()

	// 2. Relay server
	relaySrv := NewServer(Config{TokenTTL: 1 * time.Minute})
	sess, err := relaySrv.AuthorizeSession(nodeSrv.URL, "cam-1", "desktop-1")
	if err != nil {
		t.Fatalf("AuthorizeSession failed: %v", err)
	}

	handler := relaySrv.Handler()

	// 3. Client request through relay
	req := httptest.NewRequest("GET", fmt.Sprintf("/relay/stream?token=%s", sess.Token), nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "mock_video_stream_bytes" {
		t.Errorf("expected stream bytes forwarded, got %s", string(body))
	}

	if sess.BytesTransferred != int64(len("mock_video_stream_bytes")) {
		t.Errorf("expected recorded transferred bytes %d, got %d", len("mock_video_stream_bytes"), sess.BytesTransferred)
	}
}
