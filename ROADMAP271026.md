# Provision-ISR NVR support: status and roadmap (2026-10-28)

Phase 1 is merged (PR #274, merge commit `00ed808`) and deployed on this host (migration 84 to 85; web answers on 8000 and 8443). Editing the recorder's connection is on branch `feat/provision-nvr-edit`.
Original branch: `feat/provision-nvr-phase1`. Feature doc: `odd/tasks/provision-isr-nvr.md`. Plan: `docs/plans/provision-isr-integration.md`.
Test unit: a Provision-ISR `NVR8-16400AN(1U)` (TVT NVMS-9000 platform) on the local network. Its credentials are never stored in the repository.

## What the unit taught us

- The web API (`POST http://host/<command>`, XML, login = `sha512(MD5_UPPER(password) + "#" + nonce)`) works and is what the device's own web client uses.
- ONVIF works once the owner enables and saves the switch: Media 1/2, PTZ, Events, Analytics, Recording, Replay, Search, deviceIO, imaging, receiver.
- The recorder reports 14 fixed ONVIF profiles (main and sub of 7 channels) and the stream URL `rtsp://host:554/chID=<n>&streamType=main|sub&linkType=tcp`.
- RTSP playback has never been exercised (no `ffprobe` on the host, and no RTSP-to-browser path exists outside Frigate).
- Port 6036 speaks an undocumented binary protocol; it was not touched. The API Server feature is switched off on the unit.

## Done (committed on the branch)

| Task | Commit | What it delivers |
|---|---|---|
| T0 | `91f037b` | Plan and feature doc, written from the real code and the real unit |
| T1 | `9ef992e` | Migration `00085`: `frigate_servers.kind` (`frigate` default, `provision_nvr`); `Server.kind` in OpenAPI and responses |
| T2 | `71ee23c` | `internal/provisionnvr`: read-only NVMS client (login, session renewal, redacted errors, no retry of a rejected password, safe XML, size cap, `query*` only), typed reads, sanitized fixtures, fake recorder `nvmstest`. Native review: approved, no findings (3 lenses) |
| T3 | `6cd0e04` | `Detect` (NVMS plus an ONVIF probe that stays on the recorder's host), capability matrix (`confirmed`/`unknown`/`unsupported`), `POST /api/v1/servers/provision-nvr/detect` |
| T4a | `e6e91d1` | Frigate-only paths refuse recorders (409 `unsupported_for_server_kind`); Frigate event and health pollers skip them |
| T4b | `e590aa6` | `POST /api/v1/servers/provision-nvr` (register + import channels as cameras), sync, rename/move, camera removal and restore for channels |

The rest of the work (health, adding channels later, web, docs) is listed below and is committed after this file was first written.

Verified: unit tests, the integration tests of each slice, lint (0 issues), web typecheck, and read-only runs of the client and of `Detect` against the real unit (7 channels, 14 profiles).

## Also done (owner's decisions of 2026-10-28)

- **Choose some or all channels** at registration, and **add channels later**: `GET` and `POST /api/v1/servers/{id}/provision-nvr/channels` (list what the recorder has now with an `imported` flag, import the chosen ones; channels deleted here are skipped, not resurrected).
- **T4c, recorder health** (`internal/inventory/health_nvr.go`): per-channel online state, disk state, one cached session per recorder, a rejected password is reported once and never retried (a locked account is retried after 10 minutes). New problem code `recorder_disk_bad`.
- **T5, web:** third option in "Registrar servidor" (detect, pick channels, capability matrix, register); recorder edit dialog (summary, channels to add later, rename/move, sync, delete); recorder rows without the Frigate, agent and network parts; the cameras list shows "Eliminar" for a recorder channel and a delete message that says the recorder keeps recording.
- **T6, docs:** `docs/integrations/provision-isr.md` with the compatibility table.

Verified: Go unit tests, the full integration suites of `internal/inventory`, `internal/api` and `migrations` (green before the channel routes), the new integration tests for the channel routes, lint (0 issues), web typecheck, the full web test run.

## Missing

### To confirm on the real unit (needs the owner)
- Register the recorder from the UI and look at the result (channels, health, capability matrix). Nothing has been deployed: the branch is not pushed and not merged.
- Native review of the newest slices: those that include regenerated `api.gen.go` exceed the reviewer's context budget (`lens_context_budget_exceeded`).

### Open questions
1. `auth_failed` is reported through `last_error` with status `offline`, because the server status column keeps its four values. Do we want a real `auth_failed` status?

Answered since: editing a recorder's connection (address, port, HTTPS, account, password) is supported on branch `feat/provision-nvr-edit` (see `docs/integrations/provision-isr.md`).

## Live video from the recorder (Phase 1b): what the test showed and what is proposed

**What the owner saw.** Dragging a recorder camera into the live viewer shows "Servidor no disponible". The cause is not the recorder: the live viewer dials Frigate's go2rtc
and a recorder has none. It now says "El video de este NVR todavía no está disponible en OpenVMS" and stops retrying.

**What was verified on the real unit (read-only).** RTSP works with Digest: all 14 streams (7 channels, main and sub) answered `DESCRIBE`. Main streams are H.264 1920x1080 at 25 fps
(channel 1 is H.265); **sub streams are H.265 704x576 at 6 fps**. H.265 does not play in every browser through Media Source Extensions, so the grid (sub) needs a transcode or must use main.

**Built (branch `feat/nvr-live-video`, owner approved the container: "crea la go2rtc").** Add a `go2rtc` service to the Compose stack and let the live gateway use it for recorder cameras the way
it uses Frigate's go2rtc today:
1. The gateway builds the RTSP URL on the server (credentials unsealed from the stored password, never sent to the browser), registers the stream in go2rtc through its HTTP API and
   dials the same MSE websocket it already proxies. The browser protocol and the player do not change.
2. H.265 is transcoded to H.264 by go2rtc's ffmpeg source when the browser cannot play it; main stays untouched when it is already H.264.
3. go2rtc listens only on the internal network, runs as a non-root user and its image is pinned by digest. The password lives in go2rtc's memory, so its API must not be reachable from outside.
4. Cost to decide: CPU for the H.265 to H.264 transcode of the grid (6 fps at 704x576 is light), one more container, and a pinned third-party image.

Result on the real unit: through the Compose container, main (copied) and sub (H.265 converted) both play, first picture in about 4.7 s, about 1 % CPU with one viewer and none when nobody watches. Posters for recorder cameras and recorded video are still open.

Alternative that was not chosen: put ffmpeg in the API image (distroless today). It has no new container but a larger blast radius for the process that holds every session, so it is not recommended.

Not part of this proposal: recorded video (playback), which depends on Phase 4, and audio.

### Later phases (blocked on evidence)
| Phase | Item | Blocker |
|---|---|---|
| 1b | Live video from channels | RTSP playback must be proven first; needs an RTSP-to-browser path (go2rtc or the relay) that does not exist outside Frigate |
| 1b | PTZ | The unit has no PTZ camera; ONVIF PTZ service is advertised, `ptz` is `unsupported` for its profiles |
| 2 | Events (HTTP POST, ONVIF events) | Needs a payload sample from the recorder; the existing Provision LPR receiver is the pattern to extend |
| 3 | Analytics | `REQUIRES PAYLOAD SAMPLE` |
| 4 | Native SDK on port 6036 (TVT `NET_SDK`) and the API Server | `REQUIRES SDK DOCUMENTATION`. The owner's reference `docs/integrations/reference/tvt-provision-ports.md` says 6036 offers login, channels, RTSP URLs, playback, events and PTZ; the port is confirmed on our unit but the protocol was not spoken. The API Server switch is off |
| 5 | Playback of recordings | ONVIF Recording/Replay/Search are advertised, not exercised (`REQUIRES REAL DEVICE TEST`) |
| 6 | Metadata | Depends on 2 to 4 |
| - | Agent route (`/v1/provision-nvr/*`) for recorders behind firewalls | Planned together with Phase 2; the client already takes an `http.RoundTripper` |

## Risks to keep in mind
- Every wrong login counts toward the recorder's account lockout. The client, the poller and the API never resend a rejected password; the UI warns about it.
- The detect and register endpoints connect to a host chosen by a user with `servers.manage`, like the Frigate probe does. ONVIF addresses advertised by a recorder are only used for their path, on the configured host.
- Fixtures are sanitized (no serial numbers, tokens, nonce, session key or account digests); the repository never holds the unit's password.
