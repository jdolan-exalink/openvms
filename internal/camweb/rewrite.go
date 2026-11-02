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

	jsWindowTopLocRe       = regexp.MustCompile(`([^a-zA-Z0-9_$.]|^)window\.(?:top|parent)\.location\b`)
	jsTopLocRe             = regexp.MustCompile(`([^a-zA-Z0-9_$.]|^)(?:top|parent)\.location\b`)
	jsWindowTopLocAssignRe = regexp.MustCompile(`([^a-zA-Z0-9_$.]|^)window\.(?:top|parent)\.location\s*=\s*([^=])`)
	jsTopLocAssignRe       = regexp.MustCompile(`([^a-zA-Z0-9_$.]|^)(?:top|parent)\.location\s*=\s*([^=])`)
	jsLocPropRe            = regexp.MustCompile(`([^a-zA-Z0-9_$.]|^)(?:window\.|document\.)?location\.(href|protocol|assign|replace)\b`)
	jsLocAssignRe          = regexp.MustCompile(`([^a-zA-Z0-9_$.]|^)(?:window\.|document\.)?location\s*=\s*([^=]|$)`)
	jsWssPortRe            = regexp.MustCompile(`([a-zA-Z0-9_$.]+)\.wssPort\.toString\(\)`)
	// Camera UIs framed by their own pages read shared globals through window.top. In the sandboxed
	// (opaque-origin) iframe that read throws a SecurityError and kills the whole script chain, so
	// non-location cross-frame reads fall back to this frame whenever the top frame is not reachable.
	jsWindowTopPropRe = regexp.MustCompile(`([^a-zA-Z0-9_$.]|^)window\.(top|parent)\.([a-zA-Z_$][\w$]*)`)
	// A bare "parent." is deliberately not rewritten: libraries use it as an ordinary local variable (an
	// emscripten file system node, for one), and the camera's own scripts spell it window.parent.
	jsBareTopPropRe      = regexp.MustCompile(`([^a-zA-Z0-9_$.]|^)(top)\.([a-zA-Z_$][\w$]*)`)
	jsTopFrameDocumentRe = regexp.MustCompile(`([^a-zA-Z0-9_$.]|^)(?:window\.)?(top|parent)\.frames\s*\[\s*0\s*\]\.document\b`)
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
	// Avoid touching an opaque ancestor from a nested sandboxed frame. The camera's own top-level
	// page can keep its first child document; other frames fall back to their own readable document.
	var topFrameOwners []string
	body = jsTopFrameDocumentRe.ReplaceAllFunc(body, func(match []byte) []byte {
		groups := jsTopFrameDocumentRe.FindSubmatch(match)
		if len(groups) < 3 {
			return match
		}
		frameWindow := "window." + string(groups[2])
		placeholder := []byte(fmt.Sprintf("__camweb_frame_document_%d__", len(topFrameOwners)))
		topFrameOwners = append(topFrameOwners, frameWindow)
		return append(append([]byte(nil), groups[1]...), placeholder...)
	})
	// Assignment to top.location must navigate this proxy frame. Replacing only the expression
	// with __camTopLoc would overwrite the shim object instead of changing the iframe location.
	body = jsWindowTopLocAssignRe.ReplaceAll(body, []byte("${1}__camTopLoc.href = ${2}"))
	body = jsTopLocAssignRe.ReplaceAll(body, []byte("${1}__camTopLoc.href = ${2}"))
	body = jsLocPropRe.ReplaceAll(body, []byte("${1}__camLoc.${2}"))
	body = jsLocAssignRe.ReplaceAll(body, []byte("${1}__camLoc.href = ${2}"))
	// window.top.location is not this frame's address: the camera UI reads it to know how it is
	// framed (e.g. "login.htm must not be the top document"). The shim's __camTopLoc answers as
	// the camera's own top page: the session root, on this origin so same-host URLs stay proxied.
	body = jsWindowTopLocRe.ReplaceAll(body, []byte("${1}__camTopLoc"))
	body = jsTopLocRe.ReplaceAll(body, []byte("${1}__camTopLoc"))
	body = rewriteTopProps(jsWindowTopPropRe, body)
	body = rewriteTopProps(jsBareTopPropRe, body)
	for i, frameWindow := range topFrameOwners {
		window := "window.parent"
		if frameWindow == "window.top" {
			window = "__camTopW()"
		}
		placeholder := []byte(fmt.Sprintf("__camweb_frame_document_%d__", i))
		body = bytes.ReplaceAll(body, placeholder, []byte("__camFrame0Doc("+window+")"))
	}
	return jsWssPortRe.ReplaceAll(body, []byte("(${1}.wssPort?${1}.wssPort.toString():(${1}.wsPort?${1}.wsPort.toString():\"80\"))"))
}

// rewriteTopProps maps cross-frame property reads (window.top.X, top.X, window.parent.X, parent.X) to the
// frame the camera meant that is actually readable: top is the highest same-origin camera frame, parent
// the immediate one. Location access is left to the location rewrite rules (which run first). When the
// frame is not readable (opaque sandbox) both resolve to this frame, with on-demand library loading.
func rewriteTopProps(re *regexp.Regexp, body []byte) []byte {
	return re.ReplaceAllFunc(body, func(match []byte) []byte {
		groups := re.FindSubmatch(match)
		if len(groups) < 4 {
			return match
		}
		if string(groups[3]) == "location" {
			return match
		}
		resolver := "__camTopW()."
		if string(groups[2]) == "parent" {
			resolver = "__camParentW()."
		}
		out := make([]byte, 0, len(groups[1])+len(resolver)+len(groups[3]))
		out = append(out, groups[1]...)
		out = append(out, resolver...)
		out = append(out, groups[3]...)
		return out
	})
}

// shimScript wraps the calls through which a camera UI usually builds absolute-path URLs. It cannot
// see assignments to location (the browser does not allow intercepting those), so location mutations
// are rewritten to __camLoc helpers. It also shims localStorage, sessionStorage and cookie when the
// sandboxed iframe lacks allow-same-origin, and provides safe fallbacks for nested cross-origin iframe DOM accesses.
const shimScript = `<script>(function(){var P=%q;var B;try{B=new URL(document.baseURI).origin}catch(e){}
function f(u){if(!u)return u;try{var H=window.location.host;if(typeof u==="object"&&u instanceof URL){if(u.host===H||(B&&u.origin===B)){var pn=u.pathname;if(pn!==P&&pn.indexOf(P+"/")!==0)return new URL(P+pn+u.search+u.hash,window.location.href)}return u}}catch(e){}if(typeof u!=="string")return u;if(u.charAt(0)==="/"&&u.charAt(1)!=="/"){if(u!==P&&u.indexOf(P+"/")!==0)return P+u;return u}try{var H=window.location.host;var parsed=new URL(u,window.location.href);if(parsed.host===H||(B&&parsed.origin===B)){var pn=parsed.pathname;if(pn!==P&&pn.indexOf(P+"/")!==0)return P+pn+parsed.search+parsed.hash}}catch(e){}return u}
var __camProto=%p;window.__camProto=__camProto;
var __camLoc=window.__camLoc={get href(){return window.location.href},set href(u){window.location.href=f(u)},get protocol(){return __camProto||window.location.protocol},set protocol(v){try{window.location.protocol=v}catch(e){}},get host(){return window.location.host},get hostname(){return window.location.hostname},get port(){return window.location.port},get pathname(){return window.location.pathname},get search(){return window.location.search},get hash(){return window.location.hash},assign:function(u){try{window.location.assign(f(u))}catch(e){window.location.href=f(u)}},replace:function(u){try{window.location.replace(f(u))}catch(e){window.location.href=f(u)}},reload:function(){window.location.reload()}};
var __camLibs=null,__camMiss={};
function __camLoadLibs(k){try{var have={};Array.prototype.forEach.call(document.scripts,function(e){if(e.src)have[e.src.split("?")[0].split("/").pop()]=1});
if(!__camLibs){var x=new XMLHttpRequest();x.open("GET",P+"/",false);x.send();__camLibs=[];var re=/\.script\("([^"]+)"/g,m;while((m=re.exec(x.responseText))){var u=m[1].split("?")[0];if(!/jquery|global\.js|base\.js/i.test(u))__camLibs.push(u)}}
for(var i=0;i<__camLibs.length&&window[k]===undefined;i++){var u=__camLibs[i];if(__camMiss[u]||have[u.split("/").pop()])continue;__camMiss[u]=1;var r=new XMLHttpRequest();r.open("GET",P+"/"+u,false);r.send();if(r.status===200)(0,eval)(r.responseText)}}catch(e){}}
window.__camSelf=new Proxy(window,{get:function(t,k){var v=t[k];if(v===undefined&&typeof k==="string"&&!__camMiss["k:"+k]){__camMiss["k:"+k]=1;__camLoadLibs(k);v=t[k]}return typeof v==="function"?v.bind(t):v}});
function __camTopW(){var w=window;try{while(w.parent&&w.parent!==w){var p=w.parent;p.document;w=p}}catch(e){}return w!==window?w:(__camIsRoot()?window:window.__camSelf)}
function __camParentW(){try{var p=window.parent;if(p&&p!==window){p.document;return p}}catch(e){}return __camIsRoot()?window:window.__camSelf}
function __camFrame0Doc(w){try{var d=w.frames[0].document;if(d)return d}catch(e){}return window.document}
window.__camTopW=__camTopW;window.__camParentW=__camParentW;window.__camFrame0Doc=__camFrame0Doc;
function __camIsRoot(){return window.location.pathname===P+"/"}
function __camNavTop(u){try{u=f(u);var t=new URL(u,window.location.href);if(t.origin!==window.location.origin||(t.pathname!==P&&t.pathname.indexOf(P+"/")!==0))return;if(__camIsRoot())window.location.href=t.href;else window.parent.postMessage({__camTopNav:t.href},"*")}catch(e){}}
window.addEventListener("message",function(e){var d=e.data;if(d&&typeof d.__camTopNav==="string")__camNavTop(d.__camTopNav)});
var __camRoot=P+"/";window.__camTopLoc={get href(){return window.location.origin+__camRoot},set href(u){__camNavTop(u)},toString:function(){return window.location.origin+__camRoot},valueOf:function(){return window.location.origin+__camRoot},protocol:__camProto,host:window.location.host,hostname:window.location.hostname,port:window.location.port,origin:window.location.origin,pathname:__camRoot,search:"",hash:"",assign:function(u){__camNavTop(u)},replace:function(u){__camNavTop(u)},reload:function(){window.location.reload()}};
var __camNativeWS=window.WebSocket;
function __camWsUrl(u){try{var x=new URL(String(u),window.location.href);if((x.protocol==="ws:"||x.protocol==="wss:")&&x.hostname===window.location.hostname&&x.pathname!==P&&x.pathname.indexOf(P+"/")!==0)return(window.location.protocol==="https:"?"wss://":"ws://")+window.location.host+P+"/__ws/"+(x.port||(x.protocol==="wss:"?"443":"80"))+x.pathname+x.search}catch(e){}return u}
if(__camNativeWS){window.WebSocket=function(u,p){return p===undefined?new __camNativeWS(__camWsUrl(u)):new __camNativeWS(__camWsUrl(u),p)};window.WebSocket.prototype=__camNativeWS.prototype;["CONNECTING","OPEN","CLOSING","CLOSED"].forEach(function(k){window.WebSocket[k]=__camNativeWS[k]})}
var __camNativeWorker=window.Worker;
var __camWorkerPrelude="var NW=self.WebSocket;function ws(u){try{var x=new URL(String(u),D);if((x.protocol===\"ws:\"||x.protocol===\"wss:\")&&x.hostname===H&&x.pathname.indexOf(P+\"/\")!==0)return(O.indexOf(\"https:\")===0?\"wss://\":\"ws://\")+O.replace(/^https?:\\/\\//,\"\")+P+\"/__ws/\"+(x.port||(x.protocol===\"wss:\"?\"443\":\"80\"))+x.pathname+x.search}catch(e){}return u}if(NW){self.WebSocket=function(u,p){return p===undefined?new NW(ws(u)):new NW(ws(u),p)};self.WebSocket.prototype=NW.prototype}function mu(u){try{var x=new URL(String(u),D);if(x.origin===O&&x.pathname!==P&&x.pathname.indexOf(P+\"/\")!==0)x=new URL(P+x.pathname+x.search+x.hash,O);return x.href}catch(e){return u}}var NI=self.importScripts;self.importScripts=function(){var a=[];for(var i=0;i<arguments.length;i++)a.push(mu(arguments[i]));return NI.apply(self,a)};if(self.fetch){var NF=self.fetch;self.fetch=function(u,o){return NF.call(self,typeof u===\"string\"?mu(u):u,o)}}if(self.XMLHttpRequest){var XO=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(m,u){arguments[1]=mu(u);return XO.apply(this,arguments)}}var L={protocol:PR,href:D,origin:O,host:O.replace(/^https?:\\/\\//,\"\"),hostname:H,pathname:P+\"/\",search:\"\",hash:\"\"};self.__camLoc=L;self.__camTopLoc=L;self.__camTopW=function(){return self};self.__camParentW=self.__camTopW;self.__camFrame0Doc=function(){return self.document};";
if(__camNativeWorker){window.Worker=function(u,o){try{var abs=new URL(f(String(u)),window.location.href).href;var x=new XMLHttpRequest();x.open("GET",abs,false);x.send();if(x.status!==200)throw 0;var pre="(function(P,H,O,D,PR){"+__camWorkerPrelude+"})("+[P,window.location.hostname,window.location.origin,abs.replace(/[^\/]*$/,""),__camProto].map(function(v){return JSON.stringify(v)}).join(",")+");\n";return new __camNativeWorker(URL.createObjectURL(new Blob([pre,x.responseText],{type:"text/javascript"})),o)}catch(e){return new __camNativeWorker(u,o)}};window.Worker.prototype=__camNativeWorker.prototype}
function S(pfx){var c=null,at=0;function req(m,b){try{var x=new XMLHttpRequest();x.open(m,P+"/__camstate",false);x.send(b);return x.status===200?x.responseText:null}catch(e){return null}}
function all(){var n=Date.now();if(c&&n-at<250)return c;var r=req("GET");try{c=r?JSON.parse(r):(c||{})}catch(e){c=c||{}}at=n;return c}
function keys(){var a=all(),o=[];for(var k in a)if(k.indexOf(pfx)===0)o.push(k.slice(pfx.length));return o}
return{getItem:function(k){var a=all();k=pfx+k;return Object.prototype.hasOwnProperty.call(a,k)?a[k]:null},setItem:function(k,v){v=String(v);all()[pfx+k]=v;req("POST",JSON.stringify({op:"set",k:pfx+k,v:v}))},removeItem:function(k){delete all()[pfx+k];req("POST",JSON.stringify({op:"del",k:pfx+k}))},clear:function(){keys().forEach(function(k){delete all()[pfx+k]});req("POST",JSON.stringify({op:"clear",p:pfx}))},key:function(i){return keys()[i]||null},get length(){return keys().length}}};
try{window.localStorage&&window.localStorage.getItem("_")}catch(e){try{var ls=S("l:");Object.defineProperty(window,"localStorage",{get:function(){return ls},configurable:true})}catch(e2){}}
try{window.sessionStorage&&window.sessionStorage.getItem("_")}catch(e){try{var ss=S("s:");Object.defineProperty(window,"sessionStorage",{get:function(){return ss},configurable:true})}catch(e2){}}
try{document.cookie}catch(e){try{var c="";Object.defineProperty(document,"cookie",{get:function(){return c},set:function(v){c=v},configurable:true})}catch(e2){}}
var F=window.fetch;if(F)window.fetch=function(i,o){try{if(typeof i==="string")i=f(i);else if(i&&typeof i.url==="string"&&window.Request&&i instanceof Request){var u=new URL(i.url);if((u.host===window.location.host||(B&&u.origin===B))&&f(u.pathname)!==u.pathname)i=new Request(f(i.url),i)}}catch(e){}return F.call(this,i,o)};
var X=window.XMLHttpRequest;if(X){var O=X.prototype.open;X.prototype.open=function(){try{arguments[1]=f(arguments[1])}catch(e){}return O.apply(this,arguments)}}
var W=window.open;if(W)window.open=function(){try{arguments[0]=f(arguments[0])}catch(e){}return W.apply(this,arguments)};
var PS=window.history&&window.history.pushState;if(PS)window.history.pushState=function(s,t,u){try{if(typeof u==="string")u=f(u)}catch(e){}return PS.call(this,s,t,u)};
var RS=window.history&&window.history.replaceState;if(RS)window.history.replaceState=function(s,t,u){try{if(typeof u==="string")u=f(u)}catch(e){}return RS.call(this,s,t,u)};
try{var LA=window.location.assign;if(LA)window.location.assign=function(u){return LA.call(window.location,f(u))}}catch(e){}
try{var LR=window.location.replace;if(LR)window.location.replace=function(u){return LR.call(window.location,f(u))}}catch(e){}
function rw(s){if(typeof s!=="string")return s;return s.replace(/(src|href|action)=["'](\/(?!\/)[^"']*)["']/gi,function(m,a,p){return a+'="'+f(p)+'"'}).replace(/(src|href|action)=["']https?:\/\/[^\/]+(\/[^"']*)["']/gi,function(m,a,p){return a+'="'+f(p)+'"'})}
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
