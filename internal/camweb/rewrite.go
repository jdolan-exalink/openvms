package camweb

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// The camera UI is served under /camproxy/{session}/ instead of at the root of its own host, so
// absolute-path references have to be moved under that prefix. HTML and CSS bodies are rewritten
// to move paths under prefix, and JavaScript bodies (or inline scripts) are rewritten to replace
// window.top.location and related cross-frame accesses that fail in opaque sandboxes.
// Everything here is best effort and bounded by the caller's size limit.

var (
	attrRootRe   = regexp.MustCompile(`(?i)\s(?:src|href|action|data-src|poster|formaction)\s*=\s*["']/`)
	urlRootRe    = regexp.MustCompile(`(?i)url\(\s*["']?/`)
	importRootRe = regexp.MustCompile(`(?i)@import\s+["']/`)
	baseTagRe    = regexp.MustCompile(`(?i)<base\b[^>]*>`)
	headOpenRe   = regexp.MustCompile(`(?i)<head(?:\s[^>]*)?>`)
	htmlOpenRe   = regexp.MustCompile(`(?i)<html(?:\s[^>]*)?>`)

	jsWindowTopLocRe = regexp.MustCompile(`([^a-zA-Z0-9_$.]|^)window\.(?:top|parent)\.location\b`)
	jsTopLocRe       = regexp.MustCompile(`([^a-zA-Z0-9_$.]|^)(?:top|parent)\.location\b`)
	jsLocPropRe      = regexp.MustCompile(`([^a-zA-Z0-9_$.]|^)(?:window\.|document\.)?location\.(href|protocol|assign|replace)\b`)
	jsLocAssignRe    = regexp.MustCompile(`([^a-zA-Z0-9_$.]|^)(?:window\.|document\.)?location\s*=\s*([^=]|$)`)
	jsWssPortRe      = regexp.MustCompile(`([a-zA-Z0-9_$.]+)\.wssPort\.toString\(\)`)
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

func rewriteJS(body []byte) []byte {
	body = jsWindowTopLocRe.ReplaceAll(body, []byte("${1}window.location"))
	body = jsTopLocRe.ReplaceAll(body, []byte("${1}location"))
	body = jsLocPropRe.ReplaceAll(body, []byte("${1}__camLoc.${2}"))
	body = jsLocAssignRe.ReplaceAll(body, []byte("${1}__camLoc.href = ${2}"))
	return jsWssPortRe.ReplaceAll(body, []byte("(${1}.wssPort?${1}.wssPort.toString():(${1}.wsPort?${1}.wsPort.toString():\"80\"))"))
}

// shimScript wraps the calls through which a camera UI usually builds absolute-path URLs. It cannot
// see assignments to location (the browser does not allow intercepting those), so location mutations
// are rewritten to __camLoc helpers. It also shims localStorage, sessionStorage and cookie when the
// sandboxed iframe lacks allow-same-origin, and provides safe fallbacks for nested cross-origin iframe DOM accesses.
const shimScript = `<script>(function(){var P=%q;var B;try{B=new URL(document.baseURI).origin}catch(e){}
function f(u){if(!u)return u;try{var H=window.location.host;if(typeof u==="object"&&u instanceof URL){if(u.host===H||(B&&u.origin===B)){var pn=u.pathname;if(pn!==P&&pn.indexOf(P+"/")!==0)return new URL(P+pn+u.search+u.hash,window.location.href)}return u}}catch(e){}if(typeof u!=="string")return u;if(u.charAt(0)==="/"&&u.charAt(1)!=="/"){if(u!==P&&u.indexOf(P+"/")!==0)return P+u;return u}try{var H=window.location.host;var parsed=new URL(u,window.location.href);if(parsed.host===H||(B&&parsed.origin===B)){var pn=parsed.pathname;if(pn!==P&&pn.indexOf(P+"/")!==0)return P+pn+parsed.search+parsed.hash}}catch(e){}return u}
var __camProto=%p;window.__camProto=__camProto;
var __camLoc=window.__camLoc={get href(){return window.location.href},set href(u){window.location.href=f(u)},get protocol(){return __camProto||window.location.protocol},set protocol(v){try{window.location.protocol=v}catch(e){}},get host(){return window.location.host},get hostname(){return window.location.hostname},get port(){return window.location.port},get pathname(){return window.location.pathname},get search(){return window.location.search},get hash(){return window.location.hash},assign:function(u){try{window.location.assign(f(u))}catch(e){window.location.href=f(u)}},replace:function(u){try{window.location.replace(f(u))}catch(e){window.location.href=f(u)}},reload:function(){window.location.reload()}};
function S(){var s={};return{getItem:function(k){return Object.prototype.hasOwnProperty.call(s,k)?s[k]:null},setItem:function(k,v){s[k]=String(v)},removeItem:function(k){delete s[k]},clear:function(){s={}},key:function(i){return Object.keys(s)[i]||null},get length(){return Object.keys(s).length}}};
try{window.localStorage&&window.localStorage.getItem("_")}catch(e){try{var ls=S();Object.defineProperty(window,"localStorage",{get:function(){return ls},configurable:true})}catch(e2){}}
try{window.sessionStorage&&window.sessionStorage.getItem("_")}catch(e){try{var ss=S();Object.defineProperty(window,"sessionStorage",{get:function(){return ss},configurable:true})}catch(e2){}}
try{document.cookie}catch(e){try{var c="";Object.defineProperty(document,"cookie",{get:function(){return c},set:function(v){c=v},configurable:true})}catch(e2){}}
var F=window.fetch;if(F)window.fetch=function(i,o){try{if(typeof i==="string")i=f(i);else if(i&&typeof i.url==="string"&&window.Request&&i instanceof Request){var u=new URL(i.url);if((u.host===window.location.host||(B&&u.origin===B))&&f(u.pathname)!==u.pathname)i=new Request(f(i.url),i)}}catch(e){}return F.call(this,i,o)};
var X=window.XMLHttpRequest;if(X){var O=X.prototype.open;X.prototype.open=function(){try{arguments[1]=f(arguments[1])}catch(e){}return O.apply(this,arguments)}}
var W=window.open;if(W)window.open=function(){try{arguments[0]=f(arguments[0])}catch(e){}return W.apply(this,arguments)};
var PS=window.history&&window.history.pushState;if(PS)window.history.pushState=function(s,t,u){try{if(typeof u==="string")u=f(u)}catch(e){}return PS.call(this,s,t,u)};
var RS=window.history&&window.history.replaceState;if(RS)window.history.replaceState=function(s,t,u){try{if(typeof u==="string")u=f(u)}catch(e){}return RS.call(this,s,t,u)};
try{var LA=window.location.assign;if(LA)window.location.assign=function(u){return LA.call(window.location,f(u))}}catch(e){}
try{var LR=window.location.replace;if(LR)window.location.replace=function(u){return LR.call(window.location,f(u))}}catch(e){}
function rw(s){if(typeof s!=="string")return s;return s.replace(/(src|href|action)=["'](\/[^"'\/][^"']*)["']/gi,function(m,a,p){return a+'="'+f(p)+'"'}).replace(/(src|href|action)=["']https?:\/\/[^\/]+(\/[^"']*)["']/gi,function(m,a,p){return a+'="'+f(p)+'"'})}
try{var DW=document.write;if(DW)document.write=function(){try{for(var i=0;i<arguments.length;i++){if(typeof arguments[i]==="string")arguments[i]=rw(arguments[i])}}catch(e){}return DW.apply(this,arguments)}}catch(e){}
try{var DWL=document.writeln;if(DWL)document.writeln=function(){try{for(var i=0;i<arguments.length;i++){if(typeof arguments[i]==="string")arguments[i]=rw(arguments[i])}}catch(e){}return DWL.apply(this,arguments)}}catch(e){}
var SA=Element.prototype.setAttribute;Element.prototype.setAttribute=function(n,v){try{if(/^(src|href|action|data-src|poster)$/i.test(n))v=f(v)}catch(e){}return SA.call(this,n,v)};
[["HTMLImageElement","src"],["HTMLScriptElement","src"],["HTMLLinkElement","href"],["HTMLAnchorElement","href"],["HTMLIFrameElement","src"],["HTMLFormElement","action"],["HTMLSourceElement","src"],["HTMLMediaElement","src"],["HTMLEmbedElement","src"]].forEach(function(p){try{var pr=window[p[0]].prototype,d=Object.getOwnPropertyDescriptor(pr,p[1]);if(d&&d.set)Object.defineProperty(pr,p[1],{get:d.get,set:function(v){d.set.call(this,f(v))},configurable:true,enumerable:d.enumerable})}catch(e){}});
try{var cwD=Object.getOwnPropertyDescriptor(HTMLIFrameElement.prototype,"contentWindow");if(cwD&&cwD.get){var oCW=cwD.get;Object.defineProperty(HTMLIFrameElement.prototype,"contentWindow",{get:function(){var w=oCW.call(this);try{if(w&&w.document)return w}catch(e){var dE={style:{},innerHTML:"",classList:{add:function(){},remove:function(){}}};var dD={getElementById:function(){return dE},querySelector:function(){return dE},querySelectorAll:function(){return[]},body:dE};return{document:dD,style:{}}}return w},configurable:true,enumerable:cwD.enumerable})}}catch(e){}
try{var cdD=Object.getOwnPropertyDescriptor(HTMLIFrameElement.prototype,"contentDocument");if(cdD&&cdD.get){var oCD=cdD.get;Object.defineProperty(HTMLIFrameElement.prototype,"contentDocument",{get:function(){try{var d=oCD.call(this);if(d)return d}catch(e){}var dE={style:{},innerHTML:"",classList:{add:function(){},remove:function(){}}};return{getElementById:function(){return dE},querySelector:function(){return dE},querySelectorAll:function(){return[]},body:dE}},configurable:true,enumerable:cdD.enumerable})}}catch(e){}})();</script>`

// rewriteHTML moves root-relative references under prefix, drops any <base> the camera set, rewrites
// cross-frame location accesses in inline scripts, and injects one <base> (the directory of the current
// document, which is where the browser would resolve relative URLs anyway) plus the shim script, once.
// If username or password are provided, it also injects an autofill helper into the shim.
func rewriteHTML(body []byte, prefix, docDir, scheme, username, password string) []byte {
	body = baseTagRe.ReplaceAll(body, nil)
	body = prefixRoot(body, attrRootRe, prefix)
	body = rewriteCSS(body, prefix)
	body = rewriteJS(body)
	inject := []byte(`<base href="` + html.EscapeString(prefix+docDir) + `">` + shimScriptFor(prefix, scheme, username, password))
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

func shimScriptFor(prefix, scheme, username, password string) string {
	// %q yields a Go-quoted string, which is also a valid JS string for the hex-and-slash prefix.
	base := strings.Replace(shimScript, "%q", `"`+strings.NewReplacer(`\`, ``, `"`, ``, `<`, ``).Replace(prefix)+`"`, 1)
	proto := "http:"
	if scheme == "https" {
		proto = "https:"
	}
	base = strings.Replace(base, "%p", `"`+proto+`"`, 1)
	if username == "" && password == "" {
		return base
	}
	autofill := fmt.Sprintf(`;(function(){var u=%s;var p=%s;if(u||p){function af(){try{var pws=document.querySelectorAll('input[type="password"]');if(!pws.length)return false;var ok=false;for(var i=0;i<pws.length;i++){var pw=pws[i];if(p&&!pw.value){pw.value=p;pw.dispatchEvent(new Event('input',{bubbles:true}));pw.dispatchEvent(new Event('change',{bubbles:true}));ok=true;}var f=pw.form||pw.closest('form')||document;var un=f.querySelector('input[type="text"],input[name*="user" i],input[name*="login" i],input[name*="name" i],input[id*="user" i],input[id*="login" i]');if(un&&u&&(!un.value||(un.value==="admin"&&u==="admin"))){un.value=u;un.dispatchEvent(new Event('input',{bubbles:true}));un.dispatchEvent(new Event('change',{bubbles:true}));ok=true;}}return ok;}catch(e){return false;}}if(!af()){var att=0;var timer=setInterval(function(){att++;if(af()||att>30)clearInterval(timer);},400);}}})();`,
		strconv.Quote(username), strconv.Quote(password))
	return strings.Replace(base, "</script>", autofill+"</script>", 1)
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
