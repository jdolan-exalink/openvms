# Provision-ISR NVR integration: plan

Status: plan plus Phase 1 in progress (feature doc `odd/tasks/provision-isr-nvr.md`). Written against the
real code of `main` at `22ed557` and against one real recorder, probed read-only on 2026-10-27.

Rule of this document: nothing about the manufacturer's protocols is assumed. Every statement is tagged
**CONFIRMED** (seen on the real recorder or in code), or one of **UNKNOWN**, **REQUIRES REAL DEVICE TEST**,
**REQUIRES SDK DOCUMENTATION**, **REQUIRES PAYLOAD SAMPLE**.

## 1. What the recorder turned out to be

The recorder used for evidence is a Provision-ISR `NVR8-16400AN(1U)` (hardware `TD-3116H2-B1`, chip
400112), 16 IP channels, 7 cameras attached, one 2 TB disk. Its web client declares
`systemType="NVMS-9000"`, and its device list names the Provision-ISR protocol `TVT`
(`<item translateKey="Provision ISR" protocol="TVT" ...>`). So Provision-ISR NVRs of this family are a
rebrand of the TVT NVMS-9000 platform. **CONFIRMED** on this unit; other models and firmwares are UNKNOWN.

Everything below was learned from the device itself: its own web client scripts (served without login)
and read-only `query*` calls made with the credentials the owner gave for the test.

### 1.1 Integration surfaces found on the unit

| Surface | State on the unit | Evidence |
|---|---|---|
| **NVMS web API** (XML over HTTP, port 80) | Works. Full read access after login: device info, capabilities, channels, per-channel online / motion / recording status, disks. | Real responses (fixtures under `internal/provisionnvr/testdata`). |
| **ONVIF** (`/onvif/device_service`) | Did not answer: connection closed on `/onvif/device_service`, `400` with an empty body on `/device_service`. The device reports ONVIF as `enable=true, switch=false`; the meaning of `switch` is UNKNOWN, it may mean ONVIF is switched off. Turning it on is a settings change on the owner's device: not done. | `queryOnvifCfg`, probes. REQUIRES REAL DEVICE TEST (after the owner enables ONVIF). |
| **RTSP** (port 554) | Server on, `Digest` auth, anonymous access off. `OPTIONS` answers. My digest `DESCRIBE` attempts on four candidate paths all returned `401`; the stream URL format is **UNKNOWN**. Failed authentications were not repeated to avoid lockouts. | `queryRTSPServer`, probes. REQUIRES SDK DOCUMENTATION or the URL format shown by the recorder. |
| **Proprietary client port 6036** (`netPort`) | Open. Speaks a binary protocol: the first bytes are the ASCII word `head` followed by a binary header. Not documented, not touched. | Banner only. REQUIRES SDK DOCUMENTATION. |
| **API Server** (the "API Server, Authentication Base64/MD5" feature) | Present and switched **off** (`apiserverSwitch=false`, auth `Digest`). It is not what port 6036 is. | `queryApiServer`. Protocol UNKNOWN. REQUIRES SDK DOCUMENTATION. |
| **HTTP POST / alarm server** push | The web client has an alarm-server module (`AlarmCfg/alarmServer`). Not read or configured. | REQUIRES PAYLOAD SAMPLE. |
| **SNMP** | Reported supported (`supportSnmp=true`). Not tried. | `querySystemCaps`. |

### 1.2 The NVMS web API (CONFIRMED on this unit)

Framing, taken from the device's own client library:

- `POST http://<host>:<http port>/<command>` with an XML body
  `<request version="1.0" systemType="NVMS-9000" clientType="WEB"><token>…</token>…</request>`.
- Answer: `<response cmdUrl="…"><status>success|fail</status><errorCode>…</errorCode>…</response>`.
- Login is two calls. `reqLogin` returns `nonce` (a brace-wrapped GUID, kept as is), `sessionId` and a
  `token`. `doLogin` carries the user name and
  `password = sha512_hex( MD5_hex_UPPERCASE(password) + "#" + nonce )`. The MD5 hex is **uppercase**: the
  device's `SparkMD5.hash` upper-cases it, and a lowercase digest is rejected (`errorCode 536870948`).
  The response also carries a `sessionKey` AES-ECB encrypted with that MD5; the read-only calls below did
  not need it.
- Known error codes: `536870947` / `536870948` wrong user or password, `536871011` session timeout,
  `536870951` user locked, `536870943` invalid parameter.

Read-only commands verified on the unit: `queryActivationStatus` (no login), `queryBasicCfg` (name,
`productModel`, `TVTProductModel`, hardware and kernel versions), `querySystemCaps` (channel counts,
`supportRecorder`, `supportSnmp`, `supportPlateMatch`, `supportFaceMatch`, `supportFishEye`, talk, …),
`queryNodeList` (channels: id, name, camera IP and port, camera model, protocol), `queryChlStatus`
(per channel `online`, `motionStatus`, `intelligentStatus`, `recStatus`), `queryDiskStatus`,
`queryStorageDevInfo` (disks: model, size, free space, slot, RAID), `queryOnvifCfg`, `queryRTSPServer`,
`queryApiServer`, `queryNetPortCfg`.

Seen in the client but **not yet called**: `queryChlPresetList` and the cruise/trace commands (PTZ),
`queryNodeEncodeInfo` (channel encoding), `queryRecStatus`, `queryManualRecord`, playback and search
commands (`app/Rec/*`), alarm server and smart-event modules. Each needs a real call before it is used.

Sensitive data the recorder returns and that must never be stored or logged as is: the session token,
the nonce, the `sessionKey`, disk serial numbers, MAC addresses, and `queryBasicCfg` also returns a
`defaultUser` password digest.

## 2. Current OpenVMS architecture, and what to reuse

Mapped from the code on 2026-10-27.

- **Servers** live in `frigate_servers` (`migrations/00002`). There is no kind or provider column; `ServerView`
  and the OpenAPI `Server` schema are Frigate-shaped (`frigate_version`, `Capabilities` with review, preview,
  exports, lpr, face recognition, semantic search, audio, ptz). `internal/videobackend` is an unused
  Frigate-shaped abstraction: not reused.
- **Adapters**: `frigate.Adapter` (30 methods) is Frigate-bound (config edit, faces, go2rtc, reviews);
  `inventory.Adapters.Get` caches one adapter per server and is the natural place for a kind switch.
- **Sync and import**: `SyncServer` / `CreateServer` upsert cameras by `(server_id, remote_name)` through
  `UpsertCamera`, mark missing ones, and respect camera tombstones (merged in #273).
- **Health**: `HealthPoller` is Frigate-bound (`Stats`, per-camera signal, go2rtc codecs); it needs a branch
  per kind.
- **Live view** is hard-wired to Frigate's go2rtc MSE websocket (`internal/media/gateway.go`); `apps/relay`
  is a NAT forwarder that knows nothing about RTSP. A non-Frigate camera cannot be played today.
- **ONVIF** (`internal/onvif`) is complete (device info, profiles and stream URIs, snapshot, PTZ, events) and
  is reached in production through the edge agent routes `/v1/onvif/*`, with endpoint and credentials in the
  request.
- **Edge agent and Provision**: `internal/agent/provisionlpr` is a receiver + durable queue for Provision
  *cameras* pushing LPR over HTTP POST, with `events.AgentIngestor` on the central side; `camconfig` has a
  read-only `ProvisionAdapter`; docs exist under `docs/provision-isr/` (camera-level, LPR). There was **no**
  NVR support.
- **Secrets**: `secrets.Sealer` seals the server password bound to the server id; ONVIF credentials are
  redacted in Go; no per-camera credential column.
- **Web**: servers are added in `Servers.tsx` through `RegisterServer` and `ServerKindPicker` (Frigate flows).

Reuse decisions:

1. Keep `frigate_servers` as the single servers table and add a **kind** (`frigate` default,
   `provision_nvr`). A parallel table would duplicate sites, tenancy, grants, audit, cameras and groups.
2. Do **not** make the Provision client implement `frigate.Adapter`. A new narrow package
   (`internal/provisionnvr`) with its own interface; `inventory` selects by kind at the few places that need
   it (probe, sync/import, health).
3. Cameras of an NVR are ordinary `cameras` rows (so groups, zones, permissions, maps, tombstones work), with
   `remote_name` = the NVR's channel id.
4. Provision HTTP POST events (Phase 2) extend the existing agent receiver pattern instead of creating a
   second ingestion stack.

## 3. Architecture

```text
 OpenVMS Central ── API ── inventory (kind switch) ── provisionnvr client ──┐
                                                                            │ HTTP, NVMS web API
        (Phase 2+: through the Site Agent, which sits inside the customer LAN)
                                                                            ▼
                                                                  Provision-ISR NVR
```

Phase 1 talks to the NVR **from the API/worker process**. The Site Agent path (NAT, VLAN, VPN installs) is
the target for installations behind firewalls: the client takes an `http.RoundTripper`, and the agent route
(`/v1/provision-nvr/*`) is added in Phase 2 together with the event receiver, reusing the same client. This is
a deliberate staging, recorded as a risk below.

### 3.1 Package `internal/provisionnvr`

- `Client`: `Login`, session reuse with re-login on `536871011`, `Logout`, generic `Query(command, body)`;
  XML parsing with external entities disabled; response size cap; timeouts; redacted errors; never logs
  credentials, token, nonce or session key.
- Typed reads: `DeviceInfo`, `SystemCaps`, `Channels`, `ChannelStatus`, `Disks`, `ServiceConfig` (ONVIF, RTSP,
  API server, ports).
- `Detect(ctx, Endpoint, Credentials) (Detection, error)`: activation status (no login) to recognise the
  platform, then login, then the reads above, then `Capabilities`.
- `Capabilities`: derived only from what the device reported, each with a state `confirmed | unknown |
  unsupported` and a source; never from the model name.
- Error classes mapped to the states `online | degraded | offline | auth_failed | unsupported`.

### 3.2 Capability model

Capabilities are computed per recorder and drive the UI. Phase 1 values on the evidence unit:

| Capability | Value | Source |
|---|---|---|
| `webApi` | confirmed | login and queries worked |
| `channels` (list, names, online, recording, motion) | confirmed | `queryNodeList`, `queryChlStatus` |
| `storageHealth` (disk state, size, free) | confirmed | `queryDiskStatus`, `queryStorageDevInfo` |
| `onvif` | unknown (reported `enable=true`, no answer) | `queryOnvifCfg` + probes |
| `rtsp` | server on, URL format unknown | `queryRTSPServer` |
| `snmp` | reported supported, untested | `querySystemCaps` |
| `lprMatch`, `faceMatch` | reported `supportPlateMatch=true`, `supportFaceMatch=false`; events untested | `querySystemCaps` |
| `ptz`, `playback`, `events`, `nativeApi` | unknown | not called yet |

The UI shows only what is `confirmed`; `unknown` is shown as "sin verificar", never as available.

### 3.3 Data model (Phase 1)

- Migration: `frigate_servers.kind text NOT NULL DEFAULT 'frigate' CHECK (kind IN ('frigate','provision_nvr'))`;
  `capabilities jsonb` already exists and carries the Provision capability map for that kind. Additive;
  existing rows become `frigate`.
- No new camera columns in Phase 1: `remote_name` is the channel id without braces, `display_name` the
  channel name. The channel's camera address and model are not stored yet; if a later phase needs them, a
  `cameras.external_ref jsonb` is the candidate (open question, not added now).
- `base_url` keeps `scheme://host:port` of the web API. The proprietary port (6036) is stored only if a
  future phase needs it.

### 3.4 API (additive)

- `POST /api/v1/servers/provision-nvr/detect` (read-only; `/servers/provision` is already the SSH provisioning route): host, port, https flag, username, password; returns
  the detection (model, firmware, channels, capabilities, storage). Needs `servers.manage`. The password is
  not stored and not echoed.
- `POST /api/v1/servers` accepts `kind: "provision_nvr"` and the channel ids to import; `kind` absent means
  `frigate` (no behavior change for existing clients). `Server` responses gain `kind`.
- `POST /api/v1/servers/{id}/sync` for a `provision_nvr` re-reads channels.

### 3.5 Health

For `provision_nvr`, the health poller replaces the Frigate checks with: login success (`online`,
`auth_failed`, `offline`), `queryChlStatus` per channel (online, recording), `queryDiskStatus`. Interval and
backoff follow the existing poller; no stream is opened.

### 3.6 Web

"Agregar dispositivo": a third option in the server kind picker, "NVR Provision-ISR". Fields: name, host, HTTP
port, HTTPS toggle, username, password. "Detectar" shows model, firmware, channel count, capabilities (with
"sin verificar" for unknown ones) and the channel list with checkboxes; "Importar" creates the server and the
selected cameras. A server of this kind shows a "Provision" panel with status, model, firmware, last
successful connection.

## 4. Security

- Credentials: sealed with the existing `Sealer`, never returned by the API, never logged, never put in a URL.
- The NVMS login derives from an unsalted MD5 of the password; this is the device's own scheme, but it means
  the HTTP (not HTTPS) path exposes a replayable hash: prefer HTTPS where the unit has it (`httpsSwitch` is a
  setting on the unit) and recommend an isolated VLAN or the Site Agent.
- TLS: verification on by default; skipping it only through the existing explicit per-server option.
- XML: standard library decoder only, external entities and DTDs are not processed; response size capped.
- Probe and detect only issue `query*` commands. Commands that change the unit (`edit*`, `del*`, format, reset)
  are not implemented in Phase 1.
- Lockout: wrong credentials are not retried; a login failure sets `auth_failed` and stops polling until the
  password is fixed.

## 5. Phases

Phase 1 (this branch). Server kind, `internal/provisionnvr` read-only client, detection with capabilities,
import of channels as cameras, health, web add-device flow, docs and compatibility table. **Not in Phase 1,
and why:** live and recorded video (RTSP URL format unknown; ONVIF not answering; no RTSP-to-browser pipeline
for non-Frigate sources), PTZ (not called yet), events, native playback.

| Phase | Content | Blocked on |
|---|---|---|
| 2 | HTTP POST / alarm-server event receiver in the Site Agent, raw payload debugger, parser framework, event normalizer, snapshots, `/v1/provision-nvr/*` agent route | REQUIRES PAYLOAD SAMPLE; alarm-server module of the unit |
| 3 | Analytics events (motion, line crossing, zones, LPR, face, counting) from real payloads | Phase 2 samples |
| 4 | Native API / SDK (port 6036, API Server) | REQUIRES SDK DOCUMENTATION |
| 5 | Playback and hybrid recording | NVMS playback commands called for real, or SDK; stream pipeline |
| 6 | Object metadata, trajectories | real payloads |
| Video | Stream URLs and live view for NVR channels | RTSP URL format (docs or the recorder's display), or ONVIF enabled on the unit, plus a non-Frigate stream pipeline |

## 6. Risks and unknowns

- **Video is not delivered in Phase 1.** The stream URL is UNKNOWN and OpenVMS has no RTSP pipeline outside
  Frigate. This is the largest gap against the original brief.
- One unit, one firmware (web client resources `20220914.01`). Other models and firmwares are UNKNOWN; the
  client is built to tolerate absent elements and unknown channel types.
- The web API is the vendor's own browser interface, not a published integration contract: it can change with
  firmware.
- Phase 1 runs from the central process, not the Site Agent: installs behind NAT need Phase 2's agent route.
- A failed login counts toward the unit's lockout; polling must stop on `auth_failed`.
- Tombstones and the camera history purge (merged) apply to NVR cameras like any other; deleting an NVR
  camera in Phase 1 must not try to edit a Frigate config (`RemoveCamera` must be kind-aware).

## 7. Testing strategy

- Fixtures: real responses with identifiers replaced (`internal/provisionnvr/testdata`), never real
  passwords, serials, MACs, tokens or customer data.
- Client: a fake NVMS server in tests that implements `reqLogin` / `doLogin` with the real digest rule, the
  error codes, session timeout and re-login; wrong credentials, offline, not an NVMS device (`404`/HTML),
  partial answers, malformed XML, oversized body, XXE payload, unknown channel types.
- Capabilities: derived only from answers; unknown stays unknown.
- Inventory: detect, create with channel selection, sync (add, rename, missing), health states, tenancy,
  tombstones; existing Frigate paths unchanged (differential check against the base commit).
- Web: add-device flow, capability rendering, channel selection.
- Real device: a read-only run against the evidence unit before merge, recorded in the compatibility table.

## 8. Compatibility table

| Device | Firmware | Video | Events | Analytics | LPR | Face | Playback | Health | State |
|---|---|---|---|---|---|---|---|---|---|
| NVR8-16400AN(1U) (TD-3116H2-B1) | web client 20220914.01 | unknown | unknown | unknown | unknown | unknown | unknown | confirmed (read-only) | experimental |

No row is "confirmed" for a capability until it has been exercised on the device.

## Update 2026-10-28: ONVIF is on

After the owner switched ONVIF on, the recorder answers ONVIF on its web port (`/onvif/device_service`, WS-UsernameToken).
Observed: Media 1 and 2, PTZ, Events, Analytics, Recording, Replay, Search, deviceIO, imaging, receiver; 14 fixed profiles
(main and sub of 7 channels); stream URL `rtsp://host:554/chID=<n>&streamType=main|sub&linkType=tcp`. `Detect` now runs an ONVIF probe
when `queryOnvifCfg` reports the switch on, keeps every ONVIF address on the recorder's own host, and derives `onvif`, `rtsp`, `ptz`,
`playback`, `events` and `analytics` from it. A capability is `confirmed` only when it was exercised: `rtsp` stays `unknown` until a stream is
played, and `playback`, `events` and `analytics` stay `unknown` ("advertised, REQUIRES REAL DEVICE TEST"). The capability table in section 3.2
is superseded by the matrix `Detect` returns.
