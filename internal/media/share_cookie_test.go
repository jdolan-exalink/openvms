package media

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestShareCookieRoundTrip(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	exp := now.Add(time.Hour)
	v := signShareCookie("hash-a", "token-1", exp)
	if !verifyShareCookie("hash-a", "token-1", v, now) {
		t.Fatal("valid cookie rejected")
	}
}

func TestShareCookieRejects(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	exp := now.Add(time.Hour)
	v := signShareCookie("hash-a", "token-1", exp)
	parts := strings.SplitN(v, ".", 2)
	cases := map[string]struct {
		hash, token, value string
		at                 time.Time
	}{
		"expired":        {"hash-a", "token-1", v, exp.Add(time.Second)},
		"wrong token":    {"hash-a", "token-2", v, now},
		"wrong hash":     {"hash-b", "token-1", v, now},
		"empty hash":     {"", "token-1", signShareCookie("", "token-1", exp), now},
		"tampered sig":   {"hash-a", "token-1", parts[0] + "." + parts[1][:len(parts[1])-2] + "AA", now},
		"extended exp":   {"hash-a", "token-1", "1800000000." + parts[1], now},
		"garbage":        {"hash-a", "token-1", "nonsense", now},
		"empty":          {"hash-a", "token-1", "", now},
		"bad exp number": {"hash-a", "token-1", "x." + parts[1], now},
	}
	for name, c := range cases {
		if verifyShareCookie(c.hash, c.token, c.value, c.at) {
			t.Errorf("%s: cookie accepted", name)
		}
	}
}

func TestShareCookieExpiryCappedByShare(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	soon := now.Add(5 * time.Minute)
	if got := shareCookieExpiry(now, &soon); !got.Equal(soon) {
		t.Fatalf("got %v want %v", got, soon)
	}
	far := now.Add(24 * time.Hour)
	if got := shareCookieExpiry(now, &far); !got.Equal(now.Add(time.Hour)) {
		t.Fatalf("got %v", got)
	}
	if got := shareCookieExpiry(now, nil); !got.Equal(now.Add(time.Hour)) {
		t.Fatalf("got %v", got)
	}
}

func TestShareCredentialIgnoresQueryPassword(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/media/v1/public/shares/tok?password=secret", nil)
	if c := shareCredentialFromRequest(r, "tok"); c.Password != "" || c.Cookie != "" {
		t.Fatalf("query password leaked into credential: %+v", c)
	}
	r.AddCookie(&http.Cookie{Name: ShareCookieName, Value: "v"})
	if c := shareCredentialFromRequest(r, "tok"); c.Cookie != "v" || c.Password != "" {
		t.Fatalf("cookie not read: %+v", c)
	}
}

func TestSetShareAccessCookieAttributes(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/media/v1/public/shares/tok", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	setShareAccessCookie(w, r, "tok", "val", time.Now().Add(time.Hour))
	cs := w.Result().Cookies()
	if len(cs) != 1 {
		t.Fatalf("cookies: %d", len(cs))
	}
	c := cs[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/media/v1/public/shares/tok" {
		t.Fatalf("bad attributes: %+v", c)
	}
}
