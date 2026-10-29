package camweb

import (
	"bytes"
	"html"
	"net/url"
	"regexp"
	"strings"
)

// The camera UI is served under /camproxy/{session}/ instead of at the root of its own host, so
// absolute-path references have to be moved under that prefix. Only HTML and CSS bodies are touched;
// scripts are never rewritten (a fetch/XHR/window.open shim injected into HTML covers the common
// calls instead). Everything here is best effort and bounded by the caller's size limit.

var (
	attrRootRe   = regexp.MustCompile(`(?i)\s(?:src|href|action|data-src|poster|formaction)\s*=\s*["']/`)
	urlRootRe    = regexp.MustCompile(`(?i)url\(\s*["']?/`)
	importRootRe = regexp.MustCompile(`(?i)@import\s+["']/`)
	baseTagRe    = regexp.MustCompile(`(?i)<base\b[^>]*>`)
	headOpenRe   = regexp.MustCompile(`(?i)<head(?:\s[^>]*)?>`)
	htmlOpenRe   = regexp.MustCompile(`(?i)<html(?:\s[^>]*)?>`)
)

// prefixRoot inserts prefix before the final "/" of every match of re (each match ends in the "/" that
// starts an absolute path). "//host/x" (protocol-relative) and paths already under prefix are left alone.
func prefixRoot(body []byte, re *regexp.Regexp, prefix string) []byte {
	locs := re.FindAllIndex(body, -1)
	if len(locs) == 0 {
		return body
	}
	var out bytes.Buffer
	out.Grow(len(body) + len(locs)*len(prefix))
	last := 0
	already := []byte(strings.TrimPrefix(prefix, "/"))
	for _, loc := range locs {
		end := loc[1]
		rest := body[end:]
		skip := len(rest) > 0 && rest[0] == '/'
		if !skip && bytes.HasPrefix(rest, already) {
			after := rest[len(already):]
			skip = len(after) == 0 || after[0] == '/' || after[0] == '"' || after[0] == '\'' || after[0] == ')'
		}
		out.Write(body[last : end-1])
		if !skip {
			out.WriteString(prefix)
		}
		out.WriteByte('/')
		last = end
	}
	out.Write(body[last:])
	return out.Bytes()
}

func rewriteCSS(body []byte, prefix string) []byte {
	body = prefixRoot(body, urlRootRe, prefix)
	return prefixRoot(body, importRootRe, prefix)
}

// shimScript wraps the calls through which a camera UI usually builds absolute-path URLs. It cannot
// see assignments to location (the browser does not allow intercepting those). It also shims
// localStorage, sessionStorage and cookie when the sandboxed iframe lacks allow-same-origin.
const shimScript = `<script>(function(){var P=%q;function f(u){if(typeof u==="string"&&u.charAt(0)==="/"&&u.charAt(1)!=="/"&&u!==P&&u.indexOf(P+"/")!==0)return P+u;return u}
function S(){var s={};return{getItem:function(k){return Object.prototype.hasOwnProperty.call(s,k)?s[k]:null},setItem:function(k,v){s[k]=String(v)},removeItem:function(k){delete s[k]},clear:function(){s={}},key:function(i){return Object.keys(s)[i]||null},get length(){return Object.keys(s).length}}};
try{window.localStorage&&window.localStorage.getItem("_")}catch(e){try{var ls=S();Object.defineProperty(window,"localStorage",{get:function(){return ls},configurable:true})}catch(e2){}}
try{window.sessionStorage&&window.sessionStorage.getItem("_")}catch(e){try{var ss=S();Object.defineProperty(window,"sessionStorage",{get:function(){return ss},configurable:true})}catch(e2){}}
try{document.cookie}catch(e){try{var c="";Object.defineProperty(document,"cookie",{get:function(){return c},set:function(v){c=v},configurable:true})}catch(e2){}}
var B;try{B=new URL(document.baseURI).origin}catch(e){}
var F=window.fetch;if(F)window.fetch=function(i,o){try{if(typeof i==="string")i=f(i);else if(i&&typeof i.url==="string"&&window.Request&&i instanceof Request){var u=new URL(i.url);if(u.origin===B&&f(u.pathname)!==u.pathname)i=new Request(f(u.pathname)+u.search,i)}}catch(e){}return F.call(this,i,o)};
var X=window.XMLHttpRequest;if(X){var O=X.prototype.open;X.prototype.open=function(){try{arguments[1]=f(arguments[1])}catch(e){}return O.apply(this,arguments)}}
var W=window.open;if(W)window.open=function(){try{arguments[0]=f(arguments[0])}catch(e){}return W.apply(this,arguments)};
var SA=Element.prototype.setAttribute;Element.prototype.setAttribute=function(n,v){try{if(/^(src|href|action|data-src|poster)$/i.test(n))v=f(v)}catch(e){}return SA.call(this,n,v)};
[["HTMLImageElement","src"],["HTMLScriptElement","src"],["HTMLLinkElement","href"],["HTMLAnchorElement","href"],["HTMLIFrameElement","src"],["HTMLFormElement","action"],["HTMLSourceElement","src"],["HTMLMediaElement","src"],["HTMLEmbedElement","src"]].forEach(function(p){try{var pr=window[p[0]].prototype,d=Object.getOwnPropertyDescriptor(pr,p[1]);if(d&&d.set)Object.defineProperty(pr,p[1],{get:d.get,set:function(v){d.set.call(this,f(v))},configurable:true,enumerable:d.enumerable})}catch(e){}})})();</script>`

// rewriteHTML moves root-relative references under prefix, drops any <base> the camera set, and injects
// one <base> (the directory of the current document, which is where the browser would resolve relative
// URLs anyway) plus the shim script, once.
func rewriteHTML(body []byte, prefix, docDir string) []byte {
	body = baseTagRe.ReplaceAll(body, nil)
	body = prefixRoot(body, attrRootRe, prefix)
	body = rewriteCSS(body, prefix)
	inject := []byte(`<base href="` + html.EscapeString(prefix+docDir) + `">` + shimScriptFor(prefix))
	for _, re := range []*regexp.Regexp{headOpenRe, htmlOpenRe} {
		if loc := re.FindIndex(body); loc != nil {
			out := make([]byte, 0, len(body)+len(inject))
			out = append(out, body[:loc[1]]...)
			out = append(out, inject...)
			return append(out, body[loc[1]:]...)
		}
	}
	return append(inject, body...)
}

func shimScriptFor(prefix string) string {
	// %q yields a Go-quoted string, which is also a valid JS string for the hex-and-slash prefix.
	return strings.Replace(shimScript, "%q", `"`+strings.NewReplacer(`\`, ``, `"`, ``, `<`, ``).Replace(prefix)+`"`, 1)
}

// rewriteLocation maps a Location header of the camera into the prefix. ok is false when the
// redirect leaves the camera (another host, port or scheme): it must not be followed.
func rewriteLocation(loc, prefix string, tgt target) (string, bool) {
	if loc == "" {
		return "", true
	}
	if strings.HasPrefix(loc, "//") || strings.Contains(loc, "://") || strings.HasPrefix(loc, `\`) {
		raw := loc
		if strings.HasPrefix(loc, "//") {
			raw = tgt.scheme + ":" + loc
		}
		u, err := url.Parse(raw)
		if err != nil || u.User != nil {
			return "", false
		}
		host, port := tgt.host, tgt.port
		if u.Scheme != tgt.scheme || u.Hostname() != host || effectivePort(u.Scheme, u.Port()) != port {
			return "", false
		}
		path := u.EscapedPath()
		if path == "" {
			path = "/"
		}
		out := prefix + path
		if u.RawQuery != "" || u.ForceQuery {
			out += "?" + u.RawQuery
		}
		if u.Fragment != "" {
			out += "#" + u.EscapedFragment()
		}
		return out, true
	}
	if strings.HasPrefix(loc, "/") {
		if loc == prefix || strings.HasPrefix(loc, prefix+"/") {
			return loc, true
		}
		return prefix + loc, true
	}
	if strings.ContainsAny(loc, "\r\n") {
		return "", false
	}
	return loc, true // relative to the current document: already under the prefix
}

func effectivePort(scheme, port string) int {
	if port == "" {
		if scheme == "https" {
			return 443
		}
		return 80
	}
	n := 0
	for _, c := range port {
		if c < '0' || c > '9' || n > 65535 {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

var realmRe = regexp.MustCompile(`(?i)(realm\s*=\s*")([^"]*)(")`)

// scopeChallenge makes the realm of a Basic challenge unique to the session. Browsers remember Basic
// credentials per origin and realm, and every session shares the OpenVMS origin: without this, the
// password typed for one camera would be offered to the next camera that uses the same realm.
func scopeChallenge(v, sessionID string) string {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "basic") {
		return v
	}
	return realmRe.ReplaceAllString(v, `${1}${2} [cam `+sessionID[:8]+`]${3}`)
}
