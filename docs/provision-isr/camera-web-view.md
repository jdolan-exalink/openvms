# Camera web view

The camera editor can show a camera's own web interface inside OpenVMS, the way NX Witness does. It is
for settings ONVIF cannot reach. Provision-ISR cameras are the motivating case: the HTTP POST screen
(Network > Advanced > HTTP POST) that points the camera at the agent has no remote API, so the operator
fills it in from this view.

The central server usually cannot reach the camera. The request goes through the edge agent of the
camera's server, which sits on the camera LAN:

    browser -> /camproxy/{session}/ (central) -> agent /v1/camera-web/ -> camera

## How it works

1. `POST /api/v1/cameras/{cameraId}/web/session` (`createCameraWebSession`) with an optional
   `{scheme, port}` (default `http` 80). It needs `servers.manage` and configuration-secret access on
   the camera's server, the same pair as the ONVIF probe. It answers `{url, expires_at}`. 404: the
   camera has no usable address. 409: the server has no agent, or the agent is too old. 502: the agent
   could not reach the camera. An audit entry `CAMERA_WEB_SESSION` records camera, scheme and port.
2. The web UI (`CameraWebView`) loads `url` in an iframe with
   `sandbox="allow-scripts allow-forms allow-popups allow-modals"`.
3. Every request under `/camproxy/{session}/` is forwarded to the agent with the camera target in
   `X-OpenVMS-Target` headers (never in the path). The agent calls the camera and streams the answer
   back; the central rewrites it as described below.

## Threat model

The camera UI is an arbitrary device web app, and the route has no cookie authentication, so:

- **Opaque origin.** Every proxied response carries `Content-Security-Policy: sandbox allow-scripts
  allow-forms allow-popups allow-modals; frame-ancestors 'self'`, without `allow-same-origin`. Camera
  scripts therefore run in an opaque origin and cannot read OpenVMS cookies, storage or API, nor act as
  the OpenVMS user. This also holds when the link is opened in another tab. Camera CSP, X-Frame-Options,
  HSTS, Set-Cookie, Clear-Site-Data and Refresh headers are dropped; the response sets
  `Referrer-Policy: no-referrer`, `nosniff` and `Cache-Control: no-store`. Only an allow-list of
  response headers is copied.
- **Session id as credential.** 256 random bits in the path, held in memory only, bound to the user,
  tenant, camera, scheme, host and port. Lookup is by SHA-256 of the id. Unknown or expired: bare 404.
  Idle lifetime 30 minutes (sliding), absolute 8 hours, at most 4 per user (the oldest is replaced),
  1024 in total. The user's permission on the server is re-checked at least every 60 seconds; losing
  it ends the session. Expired sessions are removed lazily, with no background goroutine.
- **Server-side target.** The camera host comes from the camera record (ONVIF endpoint host, or its
  known stream host). The browser chooses only scheme and port. The host must be a literal private IPv4
  address. Dot segments and encoded separators in the path are refused, and the target never depends on
  the path.
- **Server-side cookie jar.** The camera's Set-Cookie values are kept per session and replayed to the
  camera; the browser never receives them. The OpenVMS cookies, CSRF header and bearer tokens are
  never forwarded; only a `Basic` or `Digest` Authorization header (the camera's own login prompt) is.
  The realm of Basic challenges is suffixed per session so a browser never offers one camera's password
  to another.
- **Agent second line of defence** (`internal/agent/camproxy`, route needs the `control` scope): only
  literal IPv4 in 10/8, 172.16/12 and 192.168/16. No hostnames or DNS, no loopback, link-local
  (169.254/16, cloud metadata), CGNAT (100.64/10), multicast, IPv6, or any address of the agent host
  itself. Ports 22 and the agent's own listeners are refused. The dialer re-checks the final address
  (rebinding guard). Only http and https; HTTPS skips certificate verification because cameras ship
  self-signed certificates, and that client is used by this proxy alone. Methods GET, HEAD, POST, PUT,
  DELETE, OPTIONS. Limits: request 10 MiB, response 64 MiB, connect 5 s, total 60 s. Redirects are
  returned, never followed. The environment proxy is ignored.
- **Redirects** to the camera are rewritten under the prefix; a redirect to any other host, port or
  scheme is answered with 502 and not followed.

## Rewriting and limitations

Only `text/html` and `text/css` bodies up to 5 MiB, without Content-Encoding, are rewritten:
root-relative `src`, `href`, `action`, `data-src`, `poster`, `formaction`, CSS `url(/...)` and
`@import "/..."` are moved under the prefix, `//host/...` and already-prefixed URLs are left alone, a
camera `<base>` is replaced by one pointing at the current directory, and a small script wraps `fetch`,
`XMLHttpRequest.open`, `window.open`, `setAttribute` and the `src`/`href`/`action` setters. Everything
else is streamed untouched. Because camera scripts run in an opaque origin, their XHR/fetch are
cross-origin; the route answers CORS (`Access-Control-Allow-Origin: *`, no credentials) and handles
preflights itself.

Best effort only:

- WebSocket and server-sent-event heavy UIs, and anything that needs `Upgrade`, do not work.
- JavaScript bodies are not rewritten, and `location.href = "/x"` cannot be intercepted; UIs that build
  absolute paths that way can land outside the prefix (they then 404).
- A camera that redirects http to https (or to another port) fails with 502; reopen with that scheme
  and port.
- Basic authentication prompts work in the frame; credentials are cached by the browser per session
  realm.
- Streams are capped at 60 s end to end; live video belongs in the live view.

## Configuring the Provision-ISR HTTP POST screen

1. Open the camera in the editor and press Abrir with port 80 (or the port the camera uses).
2. Log in to the camera, go to Network > Advanced > HTTP POST, and enter the agent address, port 7419,
   the keepalive path `/SendKeepalive` and the event path `/v1/provision-lpr/event` (the values the
   Provision LPR configure action shows).
3. Check the status in OpenVMS: the camera is reporting once the agent receives the keepalive.

## Deployment notes

The reverse proxy in front of the API must route `/camproxy/*` to the API, and must not overwrite the
`Content-Security-Policy` or `X-Frame-Options` of those responses with the application ones.
