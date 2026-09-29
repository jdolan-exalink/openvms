package httpx

import (
	"net/http"
	"net/url"
	"strings"
)

// OriginAllowed reports whether a websocket upgrade may proceed: requests without an Origin
// header (not a browser) and same-origin requests are accepted, as are origins listed in
// extra. It is the single origin policy shared by the media gateway and the realtime feed.
func OriginAllowed(r *http.Request, extra []string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // not a browser
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, o := range extra {
		if strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}
