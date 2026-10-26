package camweb

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	maxCookiesPerResponse = 16
	maxCookieBytes        = 4096
)

func strconvItoa(n int) string { return strconv.Itoa(n) }

func (s *session) cookieURL(path string) *url.URL {
	return &url.URL{Scheme: s.target.scheme, Host: s.target.host + ":" + strconvItoa(s.target.port), Path: path}
}

// cookieHeader builds the Cookie header the camera gets from the session's own jar.
func (s *session) cookieHeader(path string) string {
	cookies := s.jar.Cookies(s.cookieURL(path))
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// storeCookies keeps the camera's Set-Cookie values in the jar. They never reach the browser.
func (s *session) storeCookies(path string, setCookies []string) {
	if len(setCookies) == 0 {
		return
	}
	resp := &http.Response{Header: http.Header{}}
	for i, v := range setCookies {
		if i >= maxCookiesPerResponse {
			break
		}
		if len(v) <= maxCookieBytes {
			resp.Header.Add("Set-Cookie", v)
		}
	}
	s.jar.SetCookies(s.cookieURL(path), resp.Cookies())
}
