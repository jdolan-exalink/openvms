# Provision-ISR: capturing what the cameras really send

The manufacturer document confirms one real request (`POST /SendKeepalive`, empty body) and says the
format of the LPR event is not documented, so the agent never guesses it: it keeps what it cannot
parse and, on request, everything. Captures are the evidence the parser and the capability matrix
(`capability-matrix.md`) are built from.

## Modes

Set `OPENVMS_AGENT_PROVISION_CAPTURE` in the agent environment and restart it.

| Value | Keeps | Accepts POST on unknown paths |
|---|---|---|
| empty or `unparsed` (default) | event requests the parser cannot read | no |
| `all` | every event request, parsed or not (never keepalives) | yes |
| `off` | nothing | no |

Use `all` only while learning a camera's protocol: it makes the agent answer 200 to POSTs on any
path, and it logs a warning at start. Turn it off afterwards. Any other value stops the agent with an
error, so a typo cannot leave it in a mode nobody chose.

## What is stored

Each capture is a description (`<id>.json`) and the original body byte for byte (`<id>.body`) in
`$OPENVMS_AGENT_STATE_DIR/provision-captures` (default `/var/lib/openvms-agent/provision-captures`,
mode 0700, files 0600). It holds method, path, query, source IP, time, the headers without
`Authorization`, `Cookie` and similar credentials, content type, body size and SHA-256, and why it was
kept (`unparsed` or `capture_all`). The store keeps at most 200 captures and 64 MiB, removing the
oldest first. Bodies can contain plates and images: treat the directory as personal data.

If the directory cannot be created the agent logs a warning and keeps receiving events.

## Reading them

All three routes need the agent token (`Authorization: Bearer <token>`):

```text
GET /v1/provision-lpr/captures                list, newest first (up to 100)
GET /v1/provision-lpr/captures/<id>           one description
GET /v1/provision-lpr/captures/<id>/body      the original body (application/octet-stream)
```

Ask for them from the host that runs the agent, or through the central API once the camera
configuration screen exposes them (stage 1).

## Pointing a camera at the agent

The only documented way is the camera's web UI: Network > Advanced > HTTP POST, Protocol Type `API`,
Server Address and Port of the agent (default 7419), heartbeat interval. Check that `/SendKeepalive`
arrives (the camera shows online in the agent status), trigger a real plate reading, and look at the
list. Keep at least two readings, and more than one firmware or model if available, before anyone
writes a parser for the format.
