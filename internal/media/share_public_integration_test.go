//go:build integration

package media_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

const (
	sharePassword = "correct-horse-battery"
	shareClip     = "0123456789abcdef-fake-mp4-bytes"
)

type shareFixture struct {
	env    *demofix.Env
	svc    *media.Service
	jobID  uuid.UUID
	itemID uuid.UUID
}

// newShareFixture seeds one READY export job with one ready item backed by a temp file;
// a real export would need Frigate.
func newShareFixture(t *testing.T) *shareFixture {
	t.Helper()
	env := demofix.Setup(t)
	ctx := context.Background()
	clip := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(clip, []byte(shareClip), 0o600); err != nil {
		t.Fatal(err)
	}
	var cam db.GetCameraRow
	for _, c := range env.Cameras { // any demo camera will do
		cam = c
		break
	}
	f := &shareFixture{
		env: env, jobID: uuid.New(), itemID: uuid.New(),
		svc: &media.Service{Store: env.Store, Adapters: inventory.NewAdapters(env.Svc), Log: pgtest.Discard()},
	}
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
INSERT INTO export_jobs (id, tenant_id, requested_by, name, start_time, end_time, status, progress)
VALUES ($1, $2, $3, 'Share fixture', now() - interval '1 hour', now(), 'ready', 100)`,
			f.jobID, env.Demo.TenantID, env.Admin.UserID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
INSERT INTO export_job_items (id, job_id, tenant_id, camera_id, server_id, status, progress, local_path, total_bytes)
VALUES ($1, $2, $3, $4, $5, 'ready', 100, $6, $7)`,
			f.itemID, f.jobID, env.Demo.TenantID, cam.ID, cam.ServerID, clip, len(shareClip))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *shareFixture) share(t *testing.T, password string, expires *time.Time) media.ExportShare {
	t.Helper()
	sh, err := f.svc.CreateExportShare(context.Background(), f.env.Admin, f.jobID,
		media.CreateExportShareInput{Password: password, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	return sh
}

func (f *shareFixture) get(token string, cred media.ShareCredential) (media.ExportShare, error) {
	_, sh, err := f.svc.GetPublicShare(context.Background(), token, cred, nil)
	return sh, err
}

func TestPublicShareProtectedCredentials(t *testing.T) {
	f := newShareFixture(t)
	sh := f.share(t, sharePassword, nil)
	other := f.share(t, sharePassword, nil)

	if _, err := f.get(sh.ShareToken, media.ShareCredential{}); !errors.Is(err, media.ErrSharePasswordRequired) {
		t.Fatalf("empty credential: err = %v, want ErrSharePasswordRequired", err)
	}
	if _, err := f.get(sh.ShareToken, media.ShareCredential{Password: "wrong-password"}); !errors.Is(err, media.ErrSharePasswordInvalid) {
		t.Fatalf("wrong password: err = %v, want ErrSharePasswordInvalid", err)
	}
	got, err := f.get(sh.ShareToken, media.ShareCredential{Password: sharePassword})
	if err != nil {
		t.Fatalf("right password: %v", err)
	}
	cookie := got.AccessCookie
	if cookie == "" || got.AccessCookieExp.IsZero() {
		t.Fatal("right password issued no access cookie")
	}
	if strings.Contains(cookie, sharePassword) {
		t.Fatal("access cookie leaks the password")
	}
	if _, err := f.get(sh.ShareToken, media.ShareCredential{Cookie: cookie}); err != nil {
		t.Fatalf("cookie alone: %v", err)
	}
	if _, err := f.get(other.ShareToken, media.ShareCredential{Cookie: cookie}); !errors.Is(err, media.ErrSharePasswordRequired) {
		t.Fatalf("cookie for another share: err = %v, want ErrSharePasswordRequired", err)
	}
	last := cookie[len(cookie)-1]
	flipped := byte('A')
	if last == 'A' {
		flipped = 'B'
	}
	tampered := cookie[:len(cookie)-1] + string(flipped)
	if _, err := f.get(sh.ShareToken, media.ShareCredential{Cookie: tampered}); !errors.Is(err, media.ErrSharePasswordRequired) {
		t.Fatalf("tampered cookie: err = %v, want ErrSharePasswordRequired", err)
	}
}

func TestPublicShareUnprotectedIssuesNoCookie(t *testing.T) {
	f := newShareFixture(t)
	sh := f.share(t, "", nil)
	got, err := f.get(sh.ShareToken, media.ShareCredential{})
	if err != nil {
		t.Fatalf("unprotected share: %v", err)
	}
	if got.AccessCookie != "" {
		t.Fatal("unprotected share issued an access cookie")
	}
}

func TestPublicShareRevokedAndExpired(t *testing.T) {
	f := newShareFixture(t)
	ctx := context.Background()
	sh := f.share(t, sharePassword, nil)
	got, err := f.get(sh.ShareToken, media.ShareCredential{Password: sharePassword})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RevokeExportShare(ctx, f.env.Admin, sh.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.get(sh.ShareToken, media.ShareCredential{Cookie: got.AccessCookie}); !errors.Is(err, media.ErrShareRevoked) {
		t.Fatalf("revoked share with valid cookie: err = %v, want ErrShareRevoked", err)
	}

	past := time.Now().Add(-time.Minute)
	expired := f.share(t, "", &past)
	if _, err := f.get(expired.ShareToken, media.ShareCredential{}); !errors.Is(err, media.ErrShareExpired) {
		t.Fatalf("expired share: err = %v, want ErrShareExpired", err)
	}
}

func publicReq(h http.Handler, method, path, body string, cookie *http.Cookie, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func pwBody(pw string) string {
	b, _ := json.Marshal(map[string]string{"password": pw})
	return string(b)
}

func TestPublicShareHTTPFlow(t *testing.T) {
	f := newShareFixture(t)
	sh := f.share(t, sharePassword, nil)
	h := (&media.Gateway{Svc: f.svc}).Routes()
	base := "/public/shares/" + sh.ShareToken

	if w := publicReq(h, http.MethodGet, base, "", nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("GET without cookie = %d, want 401", w.Code)
	}
	if w := publicReq(h, http.MethodGet, base+"?password="+sharePassword, "", nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("GET with query password = %d, want 401 (query password must be ignored)", w.Code)
	}
	if w := publicReq(h, http.MethodPost, base, pwBody("wrong-password"), nil, nil); w.Code != http.StatusForbidden {
		t.Fatalf("POST wrong password = %d, want 403", w.Code)
	}

	w := publicReq(h, http.MethodPost, base, pwBody(sharePassword), nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("POST right password = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, sharePassword) || strings.Contains(body, "password=") {
		t.Fatal("response body leaks the password")
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == media.ShareCookieName {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value == "" {
		t.Fatal("no share access cookie set")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie HttpOnly=%v SameSite=%v, want HttpOnly and Strict", cookie.HttpOnly, cookie.SameSite)
	}
	if want := "/media/v1/public/shares/" + sh.ShareToken; cookie.Path != want {
		t.Fatalf("cookie Path = %q, want %q", cookie.Path, want)
	}

	if w := publicReq(h, http.MethodGet, base, "", cookie, nil); w.Code != http.StatusOK {
		t.Fatalf("GET with cookie = %d, want 200", w.Code)
	}

	video := base + "/items/" + f.itemID.String() + "/video"
	download := base + "/download"
	if w := publicReq(h, http.MethodGet, video, "", nil, map[string]string{"Range": "bytes=0-3"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("video without cookie = %d, want 401", w.Code)
	}
	w = publicReq(h, http.MethodGet, video, "", cookie, map[string]string{"Range": "bytes=0-3"})
	if w.Code != http.StatusPartialContent || w.Body.String() != shareClip[:4] {
		t.Fatalf("video with cookie = %d %q, want 206 %q", w.Code, w.Body.String(), shareClip[:4])
	}
	if w := publicReq(h, http.MethodGet, download, "", nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("download without cookie = %d, want 401", w.Code)
	}
	w = publicReq(h, http.MethodGet, download, "", cookie, nil)
	if w.Code != http.StatusOK || w.Body.String() != shareClip {
		t.Fatalf("download with cookie = %d, want 200 with the clip bytes", w.Code)
	}

	if err := f.svc.RevokeExportShare(context.Background(), f.env.Admin, sh.ID); err != nil {
		t.Fatal(err)
	}
	if w := publicReq(h, http.MethodGet, base, "", cookie, nil); w.Code != http.StatusGone {
		t.Fatalf("info after revoke with cookie = %d, want 410", w.Code)
	}
}
