# Edge agent: API tokens and scopes (0.4.0)

The agent answers a REST API on its port (7419, plain HTTP, and the HTTPS listener when it is
configured). OpenVMS calls it with a bearer token. Besides the token the installer writes to
`/etc/openvms/agent.token`, you can create your own tokens from the agent host.

## The legacy token

`/etc/openvms/agent.token` keeps working exactly as before and opens every route. Existing installs
and the SSH installer rely on it. The agent refuses to start without it, so it cannot be removed:
treat it as the host's root credential and give OpenVMS and other callers scoped tokens instead.

## Console commands

Run them on the agent host, as root (they write `/etc/openvms/agent.tokens.json`, mode 0600).

```text
openvms-agent token create --name NAME --scopes read,events,control,update|all [--ttl 720h]
openvms-agent token list
openvms-agent token rotate ID|NAME
openvms-agent token revoke ID|NAME
openvms-agent connection [--port 7419]
```

- `create` and `rotate` print the secret **once**, alone on stdout, so it can be piped
  (`TOKEN=$(openvms-agent token create --name ci --scopes read)`). Everything explanatory goes to
  stderr. The secret is never stored, only its SHA-256.
- `list` shows ID, name, scopes, creation and expiry. It never shows secrets.
- `rotate` keeps the name, scopes and lifetime and makes the old secret stop working at once.
- A token without `--ttl` does not expire. Names are lowercase letters, digits, `.`, `_`, `-`.
- `connection` creates the token named `openvms` (all scopes) and prints the agent version, the
  address OpenVMS should use and the token. It refuses to run if that token exists; use `token
  rotate openvms`. The printed address is the host's first LAN IPv4: replace it with the address
  OpenVMS reaches (a VPN address, or `https://name` if you publish the agent through a tunnel).
- The running agent notices changes to the file on its next request; no restart.
- The file location is `OPENVMS_AGENT_TOKENS_FILE`, or `agent.tokens.json` next to
  `OPENVMS_AGENT_TOKEN_FILE`. Every command takes `--file PATH`.

## Scopes

| Scope | Opens |
|---|---|
| `read` | `GET /v1/metrics`, `GET /v1/health` (HTTPS listener) |
| `events` | `GET /v1/provision-lpr/events[/after/N]`, `POST /v1/provision-lpr/events/ack`, `GET /v1/provision-lpr/status` |
| `control` | ONVIF discover, probe and camera operations, `POST /v1/provision-lpr/configure`, the camera registry, captures |
| `update` | `POST /v1/update` |

A valid token used on a route outside its scopes gets **403**; a missing, unknown, expired or revoked
one gets **401**. The camera-facing routes (`/SendKeepalive`, `/SendLPR`, `/LPR`,
`/Notification/LPR`, `/v1/provision-lpr/event`) carry no token, because cameras cannot send one.

An OpenVMS server needs the scopes it uses: `read` for the status panel, `events` for plate
collection, `control` for ONVIF and registration, `update` for agent updates. `all` is the simple
choice; a monitoring-only integration can use `read`.

## What changed in 0.4.0 that you should know

- The HTTPS listener (`OPENVMS_AGENT_ONVIF_TLS_LISTEN`) served the receiver's management routes
  (`status`, `registry`, `captures`, and the queued readings) **without a token**. Only the plain HTTP
  listener required one. Both listeners now mount the receiver the same way. Update agents that have
  the HTTPS listener enabled.
- Not yet: the token file is not returned or managed by OpenVMS. Registering the token in OpenVMS is
  the "connect" step (see the 0.4.0 plan).
