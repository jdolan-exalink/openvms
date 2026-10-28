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
| RTSP server and stream URL | confirmed: `rtsp://host:554/chID=<n>&streamType=main\|sub&linkType=tcp` (Digest). A real `DESCRIBE` of all 14 streams (7 channels, main and sub) answered 200 with an SDP. Detect runs one `DESCRIBE` and shows the codec | RTSP `DESCRIBE`, ONVIF `GetStreamUri`, `ffprobe` |
| Video codecs on the tested unit | main stream H.264 1920x1080 25 fps (channel 1 is H.265); **sub stream H.265** 704x576 6 fps on every channel | `DESCRIBE` and `ffprobe` |
| Live video in OpenVMS | **confirmed** through the `go2rtc` container: the live viewer plays a recorder camera (main stream copied, H.265 converted to H.264). First picture after about 4 to 5 seconds. Without the container the camera shows "El video de este NVR todavía no está disponible en OpenVMS" | gateway integration tests, and a real run through the Compose container against the unit |
| PTZ | unsupported on the tested unit (no profile has PTZ); not wired | ONVIF profiles |
| Events (HTTP POST, ONVIF events) | unknown | `REQUIRES PAYLOAD SAMPLE` |
| Analytics | unknown | `REQUIRES PAYLOAD SAMPLE` |
| Playback of recordings | **confirmed** through the recorder's own replay URL (see [Recorded video](#recorded-video)): the timeline comes from the recorder's record log, the video plays through go2rtc at normal speed | real run against the unit: 26 segments in 6 hours, a 35 s motion segment replayed in H.264 (main) and converted H.265 (sub) |
| Native API (port 6036 / API Server) | unsupported: the API Server switch is off and the protocol is undocumented | `REQUIRES SDK DOCUMENTATION` |
| SNMP, plate matching, face matching | unknown: declared by the recorder, not exercised | `querySystemCaps` |

The capability matrix on screen uses three states: **Confirmada** (seen working), **Sin verificar** (the recorder claims or advertises it,
not tested) and **No disponible**. Only confirmed features are offered.

## Live video

The live viewer plays the cameras of a recorder through a `go2rtc` container that is part of the Compose stack.

- **How it works.** When a viewer opens a recorder camera, the API reads the stored account, asks the recorder's RTSP server once for the codec (cached for 10 minutes),
  registers the stream in go2rtc and proxies go2rtc's MSE websocket to the browser, which is the same protocol and player as for Frigate cameras. The browser never
  receives the recorder's address or password.
- **Codecs.** An H.264 stream is copied without conversion. Anything else (the sub streams of the tested unit are H.265) is converted to H.264, because not every browser
  plays H.265 through Media Source Extensions. The conversion costs CPU only while someone is watching; with nobody watching go2rtc runs no ffmpeg and no connection to the recorder is open.
- **Quality.** The grid uses the sub stream; the single view uses the main stream.
- **Setting.** `GO2RTC_URL` (default `http://go2rtc:1984` in `docker-compose.yml`). Empty turns recorder video off and the viewer says it is not available.
- **Limits.** Live only: no playback of recordings (that needs the native SDK, see `ROADMAP271026.md`), no audio, no PTZ. The camera posters shown while a tile connects are not
  available for recorder cameras yet.

### The go2rtc container

- Image `alexxit/go2rtc` pinned by digest (version 1.9.9); update it deliberately.
- Runs as user 65532 with a read-only root file system, no Linux capabilities, `no-new-privileges`, 1 GiB of memory, 2 CPUs and a process limit.
- **No published port.** It sits on the `media` network, which only the API shares. go2rtc's API has no authentication and can list the stream sources, which carry the recorder's
  credentials, so nothing else may be attached to that network.
- The stream sources live in memory: go2rtc saves them to its config file, which is a tmpfs seeded from `deploy/go2rtc/go2rtc.yaml`, so they vanish when the container restarts
  and the API registers them again on the next view.
- Its RTSP and WebRTC servers are not exposed: RTSP listens on the container's loopback (go2rtc needs the module as a client) and WebRTC is off.

## Recorded video

The playback screen (and the recordings panel and the REC grid of the live view) play what the recorder itself has stored, for the cameras of a registered recorder.

- **Timeline.** It comes from the recorder's record log (`queryChlRecLog`), through the same `listCameraRecordings` answer Frigate cameras use, so the timeline looks and behaves the same. On the tested unit the log holds **motion segments** (about 35 seconds each, 67 to 214 a day per camera); a recorder set to record all the time would show long segments instead. Because a 35 second stretch is thinner than a pixel on a day-wide bar, every stretch is drawn at least 2 px wide, and each one also shows as a **Motion** marker (the recorder's own detection; no analytics are needed), in the Playback screen, the live recordings panel and the REC dock. Clicking in the future part of the day jumps to just before now.
- **REC dock.** "Ahora" opens today, 15 minutes before the current time. The day picker sits next to it. Clip export has its own framed box, "Exportación". Inicio and Fin work in two presses: the first lets the mark follow the timeline cursor (shown with a ->| or |<- badge on the timeline), the second fixes it. Fin stays locked until Inicio is fixed, neither can be pressed while the other is being placed, and Exportar is enabled only when both are fixed in order. Keys I and O do the same; Esc stops placing a mark, and a second Esc clears both. A legend under the timeline explains the bar (recorded), the thin red line (continuous recording) and the detection colours. The red line is drawn on recorded stretches of 10 minutes or more: the recorder's log does not say whether a stretch was recorded continuously, and motion-only recording comes in short pieces.
- **Days with video.** The day pickers (Playback, the live recordings panel and the REC dock) put a **red dot** on every day that has video, for the month shown (`GET /api/v1/cameras/{id}/recording-days`, permission `recordings.seek`). For a recorder it is the camera's own record log read a week at a time; for a Frigate server it is Frigate's `/api/recordings/summary`, which covers every camera of that server and can take around 15 seconds, so the answer is cached for ten minutes and the dots may appear a moment after the calendar opens. Days are counted in the browser's time zone. If the server cannot answer, the days are simply left unmarked. History goes back as far as the recorder's disk does (7 months on the tested unit).
- **Playing.** Choosing an instant starts a replay on the recorder (its own RTSP replay URL: channel, local date, local time and length) and shows it through go2rtc, like the live view. The first picture takes 2 to 5 seconds.
- **Time zones.** The record log is in UTC, but the replay URL takes the recorder's own local time. OpenVMS reads the offset from the recorder's configuration (and remembers it for 10 minutes): its time zone when it states one without daylight saving (`ART3` is three hours behind UTC), otherwise its current local time against ours, which is right only while its clock is within seven minutes of the real time. It never assumes one. Tested with a recorder at UTC-3: asking for the UTC time plays nothing, asking for the local time plays the right moment.
- **Streams.** A single camera replays the main stream; a grid of cameras replays the sub streams. H.265 is converted to H.264, as in the live view.
- **Sessions.** Every replay has its own go2rtc stream, removed when the session ends, so two people watching the same camera at different times do not interfere. Closing the page stops the replay on the recorder.
- **Access.** Needs `recordings.view` on the camera; the timeline needs `recordings.seek`. Every replay is audited (`PLAYBACK_VIEWED`) with its start and end.

- **Export.** The Exportar button of the REC dock works for recorder cameras too: the worker copies the recorder's own replay (ffmpeg, video copied without re-encoding, audio left out) into the export job, so it needs ffmpeg in the worker image and no Frigate. The recorder serves its replay no faster than real time, so an export takes at least as long as the clip: measured on the tested unit, a 40 second clip took about 60 seconds. At most two cameras of one recorder are copied at a time; the others wait. Progress follows the video written. A range with no recording at all fails with "the recorder has no recording in that range"; a range that runs past the last recording is exported up to where the video ends (ffmpeg waits 10 seconds for more before it finishes); the gaps inside a range are not filled. The older single-camera export (Playback) is still not available for recorders and answers 409 `unsupported_for_server_kind`.

Limits: the recorder replays at normal speed and cannot play backwards, so there is no speed control and no reverse; jumping to another instant (timeline or "go to") starts a new session. Cameras of a recorder are not kept in step with each other in the multi-camera view: each replays from the instant chosen. Pausing from the REC grid's shared controls does not reach a recorder camera (the player's own controls do). Exporting a clip from a recorder camera works from the REC dock only (see Export above); downloading a single clip from the Playback screen is not available yet.

## Channel numbers

The recorder names channels three ways: a GUID (what OpenVMS stores as the camera's identity), the number the web client shows, and the number in RTSP and
ONVIF (`chID=<n>`, `Profile_<n>_<stream>`). The GUID's first field carries the RTSP/ONVIF number in hexadecimal (`0000000A-...` is channel 10), so OpenVMS
derives the number from the id. On the tested unit the ids 1, 7, 8, 9, A, B, C matched the ONVIF channels 1, 7, 8, 9, 10, 11, 12, and every one of them answered an RTSP
`DESCRIBE`. It was not compared image by image, so it is `REQUIRES REAL DEVICE TEST` that each number shows the same camera on both sides. The numbers are not contiguous
(channels 2 to 6 are unused slots), and the camera names ("Cámara IP 01" is channel 7) do not follow the numbers.

## Compatibility table

| Model | Platform | Firmware | Web API | ONVIF | Channels and health | Video | PTZ | Events | Status |
|---|---|---|---|---|---|---|---|---|---|
| `NVR8-16400AN(1U)` (`TD-3116H2-B1`) | TVT NVMS-9000 | `1.4.7.61583B230726` | confirmed | confirmed | confirmed | live confirmed (go2rtc); recordings not | unsupported on this unit | unknown | partial |
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
| The camera says the video is not available | The `go2rtc` container is not running or `GO2RTC_URL` is empty; check `docker compose ps go2rtc` |
| The camera says the recorder rejected the credentials | The stored password is outdated: edit the server's connection tab |
| The timeline is empty but the recorder has video | The recorder is not reachable, or the recording is not within the day shown; check the server's status |
| The recording still shows the old player or the timeline stays empty after an update | The web app is a PWA: accept the "update available" banner (or reload twice) to load the new version |
| A replay shows nothing or starts at the wrong moment | The recorder's clock or time zone changed; sync the server (the clock offset is read again within 10 minutes) |

## Next phases

Video, PTZ, events, analytics, native API, playback and metadata are blocked on evidence, not on code. They are listed with their blockers in
`ROADMAP271026.md`.
