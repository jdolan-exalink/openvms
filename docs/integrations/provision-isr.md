# Provision-ISR recorders (NVR)

OpenVMS can register a Provision-ISR network video recorder as a server, next to Frigate servers. A recorder is a server of kind
`provision_nvr`; each channel you import becomes an ordinary camera that remembers its recorder.

**Phase 1 is read-only.** OpenVMS logs in, reads the recorder and keeps its channels, health and capabilities. It does not play video,
move PTZ cameras, receive events or play back recordings yet (see [What works today](#what-works-today)). Nothing is ever written to the
recorder.

Tested on: `NVR8-16400AN(1U)`, hardware `TD-3116H2-B1`, firmware `1.4.7.61583B230726`, TVT NVMS-9000 platform, 16 IP channels.
See `docs/plans/provision-isr-integration.md` for the design, `ROADMAP271026.md` for the status and `docs/integrations/reference/tvt-provision-ports.md` for the owner-supplied port reference (native SDK on 6036 for recorders, 9008 for cameras) with what our unit confirmed.

## Before you add a recorder

1. The recorder must be reachable from the OpenVMS server (HTTP port 80 by default, or HTTPS).
2. Use an account that can log in remotely. Keep the password handy: **a wrong password is not retried**, because every failed login counts
   toward the recorder's own account lockout.
3. Enable ONVIF on the recorder (web client: network settings, ONVIF) and **save** it. Without it OpenVMS can still list channels and read
   health, but it cannot read the stream URLs or the ONVIF capabilities.

## Add a recorder

Settings, Servers, **Registrar servidor**, then **Registrar un NVR Provision-ISR**.

1. Fill in the site, a name, the recorder's address, the web port (empty means 80, or 443 with HTTPS), the account and the password.
2. **Detectar** logs in once and reads the recorder. The screen shows model, firmware, every channel (with its camera address and whether it
   is online) and the capability matrix.
3. Pick the channels that become cameras (all of them are selected at first) and press **Registrar NVR**.

The password is stored encrypted (AES-256-GCM, bound to the server row) and never returned by the API.

## Channels

- **Choose some or all** when you register, as above.
- **Add channels later** (for example when someone connects a new camera to the recorder): edit the server, tab **Canales a importar**,
  **Leer canales del NVR**, tick the new channels, **Agregar los elegidos**. Channels that are already cameras are shown as such and cannot
  be picked twice.
- **Sync** (tab **Acciones**) re-reads the recorder, refreshes its capabilities and the cameras already imported, and marks a camera whose
  channel disappeared from the recorder ("Ya no aparece en el NVR"). It does not add channels; use the tab above for that.
- **Delete a camera** (Cameras list, **Eliminar**) removes it from OpenVMS only. The recorder is not touched and keeps recording that
  channel. A deleted channel is not brought back by sync or by adding channels; restore it from the server's deleted cameras instead.
- A camera's name is the channel's name when it is imported. Its identity is the channel id, so renaming the channel on the recorder does not
  create a second camera.

## Health

OpenVMS polls each recorder with one cached login: per-channel online state and disk state. No stream is opened.

| Situation | Server | Cameras |
|---|---|---|
| Everything answers, all channels online | `online` | `online` |
| A channel is offline | `degraded`, "Sin señal: <channel names>" | that channel `offline` |
| The recorder reports a disk as bad | `degraded`, "El NVR informa un disco dañado" | unchanged |
| The recorder cannot be reached | `offline`, `last_error` starts with `offline:` | `unknown` |
| The stored password was rejected | `offline`, `last_error` starts with `auth_failed:`; polling stops until the password changes | `unknown` |
| The account is locked | like `auth_failed`; tried again after 10 minutes | `unknown` |
| The device is not an NVMS-9000 recorder | `offline`, `last_error` starts with `unsupported:` | `unknown` |

## Edit a recorder

Edit the server, tab **Conexión**: name, site, address, web port, HTTPS, account and password. A blank password keeps the stored one (changing
the account requires typing its password). When the address, the account or the password changes, OpenVMS first connects with the new
settings and saves only if the recorder answers **and** it is still the same recorder, meaning at least one imported channel exists on it. A
wrong password is refused after one attempt and nothing is saved. Entering the right password after a rejection restores the polling.

The authentication mode, the certificate switch and event alarm modes of Frigate servers do not exist for recorders; the API refuses them.

## What works today

| Capability | State | How it was checked |
|---|---|---|
| Web API login and reads | confirmed | real recorder, read-only |
| Channel list (name, camera address, model) | confirmed | real recorder |
| Channel online / motion / recording state | confirmed | real recorder |
| Disk state, size, recording date range | confirmed | real recorder |
| ONVIF services and 14 media profiles | confirmed | real recorder, after enabling ONVIF |
| RTSP stream URL format | partial: the recorder reports `rtsp://host:554/chID=<n>&streamType=main\|sub&linkType=tcp`; playback not exercised | ONVIF `GetStreamUri` |
| Live video in OpenVMS | unsupported yet | needs an RTSP-to-browser path outside Frigate |
| PTZ | unsupported on the tested unit (no profile has PTZ); not wired | ONVIF profiles |
| Events (HTTP POST, ONVIF events) | unknown | `REQUIRES PAYLOAD SAMPLE` |
| Analytics | unknown | `REQUIRES PAYLOAD SAMPLE` |
| Playback of recordings | unknown (ONVIF Recording, Replay and Search are advertised) | `REQUIRES REAL DEVICE TEST` |
| Native API (port 6036 / API Server) | unsupported: the API Server switch is off and the protocol is undocumented | `REQUIRES SDK DOCUMENTATION` |
| SNMP, plate matching, face matching | unknown: declared by the recorder, not exercised | `querySystemCaps` |

The capability matrix on screen uses three states: **Confirmada** (seen working), **Sin verificar** (the recorder claims or advertises it,
not tested) and **No disponible**. Only confirmed features are offered.

## Compatibility table

| Model | Platform | Firmware | Web API | ONVIF | Channels and health | Video | PTZ | Events | Status |
|---|---|---|---|---|---|---|---|---|---|
| `NVR8-16400AN(1U)` (`TD-3116H2-B1`) | TVT NVMS-9000 | `1.4.7.61583B230726` | confirmed | confirmed | confirmed | unknown | unsupported on this unit | unknown | partial |
| Other Provision-ISR / Ossia models | NVMS-9000 family | - | unknown | unknown | unknown | unknown | unknown | unknown | unknown |

Allowed states: `confirmed | partial | experimental | unsupported | unknown`. A model is added here only after it was read on a real unit.

## Security

- The password is sealed with the same sealer as Frigate servers, never logged, never returned by the API, and never written into a URL.
- TLS certificates are verified whenever HTTPS is used; there is no switch to skip verification.
- The client only sends the login handshake and `query*` commands. Any other command is refused before it reaches the network.
- Answers are parsed as XML without external entities and are capped in size.
- ONVIF addresses that the recorder advertises are used for their path only, on the host you configured, so a recorder cannot send OpenVMS's
  credentials to another host.
- Registering and detecting need `servers.manage` on the site. The detect call connects to a host chosen by that user, like the Frigate
  probe does.

## API

| Route | Purpose |
|---|---|
| `POST /api/v1/servers/provision-nvr/detect` | Read a recorder without saving it |
| `POST /api/v1/servers/provision-nvr` | Register it and import the chosen channels |
| `GET /api/v1/servers/{serverId}/provision-nvr/channels` | Read the channels the recorder has now, marking the imported ones |
| `POST /api/v1/servers/{serverId}/provision-nvr/channels` | Import more channels |
| `POST /api/v1/servers/{serverId}/sync` | Re-read the recorder |

Recorder failures answer 502 with a code: `nvr_unreachable`, `nvr_auth_failed`, `nvr_locked` or `nvr_unsupported`. They never answer 401, which
would sign the user out of OpenVMS. Operations that only exist for Frigate (restart, configuration) answer 409 `unsupported_for_server_kind`
for a recorder and never contact it.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `nvr_unreachable` | Wrong address or port, a firewall, or the recorder is off |
| `nvr_auth_failed` | Wrong username or password. Check it on the recorder before trying again |
| `nvr_locked` | Too many failed logins; wait before retrying |
| `nvr_unsupported` | The address answers, but it is not an NVMS-9000 recorder, or it is not activated |
| ONVIF shows "No disponible" | The ONVIF switch is off or was not saved on the recorder |
| `rtsp` shows "Sin verificar" | Expected until video playback is implemented |

## Next phases

Video, PTZ, events, analytics, native API, playback and metadata are blocked on evidence, not on code. They are listed with their blockers in
`ROADMAP271026.md`.
