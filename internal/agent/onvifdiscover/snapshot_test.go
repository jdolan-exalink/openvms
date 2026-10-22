package onvifdiscover

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFetchOnvifSnapshotSecurityPolicy(t *testing.T) {
	var validJPEG bytes.Buffer
	if err := jpeg.Encode(&validJPEG, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	jpegBytes := validJPEG.Bytes()
	for _, tc := range []struct {
		name        string
		uri         string
		username    string
		status      int
		contentType string
		body        []byte
		wantErr     bool
	}{
		{name: "valid jpeg", uri: "http://192.0.2.10/snapshot.jpg", status: http.StatusOK, contentType: "image/jpeg", body: jpegBytes},
		{name: "https credentialed jpeg", uri: "https://192.0.2.10/snapshot.jpg", username: "camera", status: http.StatusOK, contentType: "image/jpeg", body: jpegBytes},
		{name: "credentials require https", uri: "http://192.0.2.10/snapshot.jpg", username: "camera", status: http.StatusOK, contentType: "image/jpeg", body: jpegBytes, wantErr: true},
		{name: "different host rejected", uri: "http://192.0.2.11/snapshot.jpg", status: http.StatusOK, contentType: "image/jpeg", body: jpegBytes, wantErr: true},
		{name: "redirect rejected", uri: "http://192.0.2.10/snapshot.jpg", status: http.StatusFound, contentType: "image/jpeg", wantErr: true},
		{name: "non image rejected", uri: "http://192.0.2.10/snapshot.jpg", status: http.StatusOK, contentType: "text/html", body: []byte("not a jpeg"), wantErr: true},
		{name: "malformed jpeg rejected", uri: "http://192.0.2.10/snapshot.jpg", status: http.StatusOK, contentType: "image/jpeg", body: []byte("bad"), wantErr: true},
		{name: "userinfo rejected", uri: (&url.URL{Scheme: "http", User: url.UserPassword("camera", "secret"), Host: "192.0.2.10", Path: "/snapshot.jpg"}).String(), status: http.StatusOK, contentType: "image/jpeg", body: jpegBytes, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tc.status,
					Header:     http.Header{"Content-Type": []string{tc.contentType}},
					Body:       io.NopCloser(strings.NewReader(string(tc.body))),
					Request:    r,
				}, nil
			})
			got, err := fetchOnvifSnapshot(context.Background(), "192.0.2.10", tc.uri, tc.username, "pw", transport)
			if (err != nil) != tc.wantErr {
				t.Fatalf("fetchOnvifSnapshot error = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && !bytes.Equal(got, jpegBytes) {
				t.Fatalf("snapshot = %x, want %x", got, jpegBytes)
			}
		})
	}
}

func TestFetchOnvifSnapshotSendsBasicAuthOnlyToHTTPSAndStopsRedirects(t *testing.T) {
	var calls int
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.Header.Get("Authorization") == "" {
			t.Errorf("credentialed request was not HTTPS with Basic auth: %s, %v", r.URL.Scheme, r.Header)
		}
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"http://192.0.2.11/redirect"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	})
	_, err := fetchOnvifSnapshot(context.Background(), "192.0.2.10", "https://192.0.2.10/snapshot.jpg", "camera", "pw", transport)
	if err == nil || calls != 1 {
		t.Fatalf("err = %v, transport calls = %d; want redirect error after one call", err, calls)
	}
}

func TestFetchOnvifSnapshotAppliesRequestDeadline(t *testing.T) {
	var deadline time.Time
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		deadline, _ = r.Context().Deadline()
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("bad")), Request: r}, nil
	})
	_, _ = fetchOnvifSnapshot(context.Background(), "192.0.2.10", "http://192.0.2.10/snapshot.jpg", "", "", transport)
	if deadline.IsZero() || time.Until(deadline) > probeOperationTimeout || time.Until(deadline) <= 0 {
		t.Fatalf("request deadline = %v, want within %v", deadline, probeOperationTimeout)
	}
}

func TestFetchOnvifSnapshotBoundsBody(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", maxOnvifSnapshotBytes+1))),
			Request:    r,
		}, nil
	})
	if _, err := fetchOnvifSnapshot(context.Background(), "192.0.2.10", "http://192.0.2.10/snapshot.jpg", "", "", transport); err == nil {
		t.Fatal("oversized response accepted")
	}
}
