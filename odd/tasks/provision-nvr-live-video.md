# Live video from Provision-ISR recorders through a go2rtc container

Objective: the live viewer plays the cameras of a registered Provision-ISR recorder. Builds on `odd/tasks/provision-isr-nvr.md` (Phase 1) and PR #276.
Branch: `feat/nvr-live-video` (stacked on `feat/nvr-live-honesty`). Delivery strategy: `ask-on-risk`. Runner: `go test ./internal/...`, `go test -tags integration`, `pnpm --dir apps/web test`.
Status: started 2026-10-28.

## Specs

- S1. "cuando arrastro la nvir me dice servidor no disponible" and "dale subi y crea pr y crea la go2rtc": dragging a recorder camera into the live viewer must play its video, using a go2rtc container as proposed (internal network, non-root, image pinned by digest).
- S2. Same player and browser protocol as Frigate cameras: the gateway proxies go2rtc's MSE websocket; the browser never sees the recorder's address or credentials.
- S3. The recorder's password is unsealed on the server for the call only, lives in go2rtc's memory (tmpfs), and never appears in logs, errors, API answers, audit rows or the browser.
- S4. H.265 streams are delivered as H.264 (the sub streams of the tested unit are H.265); an H.264 main stream is copied without transcoding.
- S5. go2rtc is reachable only by the API over a private network; its API, RTSP and WebRTC servers are not published and not exposed on other interfaces.
- S6. When go2rtc is not configured or not reachable, the viewer keeps saying clearly that the video is not available (no endless retry).
- S7. Nothing is written to the recorder; one RTSP connection per active stream, none while nobody watches.

## Tasks

- [ ] T1 (S2, S3) `internal/go2rtc` client: register a stream, build the websocket target, no secret in errors. Route: inline.
- [ ] T2 (S3, S4, S7) Recorder video provider in `internal/media`: RTSP source from the stored account and channel number, codec probe with a cache, copy vs transcode, stream registration cache. Route: inline.
- [ ] T3 (S1, S2, S6) Gateway live handler uses the provider for recorder cameras; wiring in `apps/api`. Risk: item 2 (credentials) and shared live path. Route: inline.
- [ ] T4 (S5) Compose: `go2rtc` service, seed config, private network, health check, api env; deploy docs. Risk: item 5 (deployment). Route: inline.
- [ ] T5 (S1) Docs, roadmap, capability matrix wording; verify against the real unit.

## Log

- L1 (2026-10-28, owner): "cuando arrastro la nvir me dice servidor no disponible, verifica todo lo que tenemos"; then, after the go2rtc proposal in `ROADMAP271026.md`: "dale subi y crea pr y crea la go2rtc".
- L2 (2026-10-28): experiment on the real unit with a throwaway go2rtc 1.9.9 container (`alexxit/go2rtc@sha256:7d31dcb73633310e80497c431f4534c81ed55ec0708db3262bc56918e654bba5`, non-root, read-only root, no capabilities). Findings that shape the design: (a) the `rtsp` module must stay enabled because it is also the RTSP client; an empty `rtsp.listen` makes every RTSP or ffmpeg source fail with "rtsp module disabled"; (b) a direct `rtsp://` source negotiated no codecs against the unit, while `ffmpeg:rtsp://...` sources worked, so every recorder source uses `ffmpeg:`; (c) `#video=copy` of an H.264 main stream gave media in 3.8 s, an H.265 sub stream with `#video=h264` in 6.5 s, an H.265 main with `#video=h264` in 3.8 s; (d) `PUT /api/streams` tries to persist the stream into the config file and answers 400 when the file is not writable (the stream still registers), so the config file must be on a tmpfs; (e) a second `-config` with inline YAML is read as a path and ignored: the seed config is a file; (f) default listeners (RTSP 8554, WebRTC 8555) open on every interface unless the seed config narrows them; (g) idle cost: about 10 MB and no CPU.
