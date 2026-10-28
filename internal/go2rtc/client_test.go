package go2rtc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPutSendsTheNameAndTheEncodedSource(t *testing.T) {
	var gotMethod, gotName, gotSrc string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotMethod, gotName, gotSrc = r.Method, r.URL.Query().Get("name"), r.URL.Query().Get("src")
		if r.URL.Path != "/api/streams" {
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer srv.Close()
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	src := "ffmpeg:rtsp://admin:" + "p&ss=1" + "@10.0.0.2:554/chID=7&streamType=main&linkType=tcp#video=copy" // #nosec G101 -- test value
	if err := c.Put(context.Background(), "nvr-abc_main", src); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotName != "nvr-abc_main" || gotSrc != src {
		t.Fatalf("go2rtc got %s name=%q src=%q", gotMethod, gotName, gotSrc)
	}
}

func TestErrorsNeverCarryTheSource(t *testing.T) {
	const secret = "S3cretPass"
	src := "ffmpeg:rtsp://admin:" + secret + "@10.0.0.2/x#video=copy"

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "config says "+r.URL.Query().Get("src"), http.StatusBadRequest)
	}))
	defer failing.Close()
	c, _ := New(failing.URL)
	err := c.Put(context.Background(), "s", src)
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), secret) {
		t.Fatalf("a 400 must be ErrUnavailable without the source, got %v", err)
	}

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := dead.URL
	dead.Close()
	c, _ = New(url)
	err = c.Put(context.Background(), "s", src)
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "admin") {
		t.Fatalf("a connection failure must be ErrUnavailable without the source, got %v", err)
	}
}

func TestDeleteRemovesTheStreamAndToleratesOneThatIsGone(t *testing.T) {
	var gotMethod, gotSrc string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotSrc = r.Method, r.URL.Query().Get("src")
		w.WriteHeader(status)
	}))
	defer srv.Close()
	c, _ := New(srv.URL)
	if err := c.Delete(context.Background(), "nvr-1-rec-ab12"); err != nil || gotMethod != http.MethodDelete || gotSrc != "nvr-1-rec-ab12" {
		t.Fatalf("Delete: %v, sent %s src=%q", err, gotMethod, gotSrc)
	}
	status = http.StatusNotFound
	if err := c.Delete(context.Background(), "gone"); err != nil {
		t.Fatalf("a stream that is already gone is not an error, got %v", err)
	}
	status = http.StatusInternalServerError
	if err := c.Delete(context.Background(), "x"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a server error must be ErrUnavailable, got %v", err)
	}
	if err := c.Delete(context.Background(), "a b"); err == nil {
		t.Fatal("an invalid name must be refused")
	}
}

func TestPingAndWebSocketURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, _ := New(srv.URL)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := c.WebSocketURL("nvr-1_sub")
	if err != nil || got != "ws://"+strings.TrimPrefix(srv.URL, "http://")+"/api/ws?src=nvr-1_sub" {
		t.Fatalf("WebSocketURL = %q, %v", got, err)
	}
	tls, _ := New("https://go2rtc:1984")
	if got, _ := tls.WebSocketURL("a"); got != "wss://go2rtc:1984/api/ws?src=a" {
		t.Fatalf("https must give wss, got %q", got)
	}
}

func TestInvalidNamesAndURLsAreRefused(t *testing.T) {
	c, _ := New("http://go2rtc:1984")
	for _, name := range []string{"", "a b", "a/b", "a?src=x", strings.Repeat("a", 101), "ñ"} {
		if err := c.Put(context.Background(), name, "x"); err == nil {
			t.Fatalf("Put(%q) must be refused", name)
		}
		if _, err := c.WebSocketURL(name); err == nil {
			t.Fatalf("WebSocketURL(%q) must be refused", name)
		}
	}
	for _, bad := range []string{"", "go2rtc:1984", "ftp://x", "http://u:p@x", "http://x/path", "http://x?y=1"} {
		if _, err := New(bad); err == nil {
			t.Fatalf("New(%q) must be refused", bad)
		}
	}
}
