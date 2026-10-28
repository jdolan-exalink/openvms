# Recorded video from Provision-ISR recorders

Objective: the playback screen shows the recordings of a registered recorder's cameras. Builds on `odd/tasks/provision-nvr-live-video.md` (PR #277, the go2rtc container).
Branch: `feat/nvr-playback`. Delivery strategy: `ask-on-risk`. Runner: `go test ./internal/...`, `go test -tags integration`, `pnpm --dir apps/web test`.
Status: T1-T5 built and verified on the real unit; not pushed, not merged, not deployed.

## Specs

- S1. "el live se ve bien, el grabado no": the recordings that live inside the recorder play in OpenVMS' playback screen, for the cameras of a registered Provision-ISR recorder.
- S2. The timeline comes from the recorder's own record log, through the same `listCameraRecordings` answer the Frigate cameras use, so the timeline code does not change.
- S3. Times: the recorder's log is in UTC, but its replay URL takes the recorder's local time. The offset is read from the device, never assumed.
- S4. The browser never receives the recorder's address or password. Every playback session has its own go2rtc stream, removed when the session ends.
- S5. Honest limits: playback at normal speed only (the recorder reports `ReversePlayback=false` and streams at 1x), seeking starts a new session at the chosen time, and no export or download for recorder cameras yet.
- S6. A recorder that is unreachable, a time without recordings, or a refused credential are reported with the same error codes the live viewer uses.

## Tasks

- [x] T1 (S2, S3) `internal/provisionnvr`: record log (`queryChlRecLog`), clock offset (`queryTimeCfg`), replay path; fixtures. Route: inline.
- [x] T2 (S2, S3, S4) `media`: recorder session cache, `Recordings` for recorder cameras, replay stream in go2rtc with cleanup. Route: inline.
- [x] T3 (S1, S4, S6) Gateway websocket `GET /media/v1/cameras/{id}/playback`. Risk: item 2 (credentials) and a path shared with live. Route: inline.
- [x] T4 (S1, S5) Web: playback screens use the replay player for recorder cameras; no speed control. Route: inline.
- [x] T5 Docs, roadmap; verify on the real unit.

## Log

- L1 (2026-10-28, owner): "el live se ve bien, el grabado no".
- L2 (2026-10-28): evidence from the real unit, read-only. ONVIF: the recorder lists `Record_<ch>_<stream>` recordings, `GetRecordingSummary` says data from 2026-04-07 to now, Replay reports `RTP_RTSP_TCP=true` and `ReversePlayback=false`, Search has no event or metadata search. `GetReplayUri` returns the format `rtsp://host:554/chID=<n>&date=YYYY-MM-DD&time=HH:MM:SS&timelen=<seconds>&streamType=main|sub&linkType=tcp`. The NVMS web API, from the device's own playback script, offers `queryDatesExistRec` (days with recordings), `queryChlsExistRec` and `queryChlRecLog` (segments: type, start, end, size; `recList timeZone="UTC"`). `queryTimeCfg` says timezone `ART3`, no daylight saving, and the current local time. Test with a known 37 s motion segment of channel 7 (07:27:41 local, 10:27:41 UTC): the replay URL with the **local** time delivered 36.4 s of video at 25 fps at about 1x speed; with the UTC time it delivered nothing and hung. In the last 3 hours the log held only MOTION segments, so the timeline is made of motion segments on this unit.
- L3 (2026-10-28): built. `provisionnvr.RecordLog`, `UTCOffset` (from the recorder's stated time zone, e.g. `ART3`; its current local time against ours, rounded to 15 minutes, only when daylight saving is on or the zone is not understood: a first version used the clock only, and a unit test caught that it depended on the time of day and on the recorder's clock being right) and `ReplayPath`; `media.NVRVideo.Recordings` and `OpenReplay` (one go2rtc stream per session, removed by `Close`; one cached login per recorder; clock offset cached 10 minutes); `media.Service.Recordings` serves recorder cameras (so `listCameraRecordings` and the timeline work unchanged); gateway websocket `GET /media/v1/cameras/{id}/playback` sharing the relay with live (permission `recordings.view`, window validated to at most a day, audited as `PLAYBACK_VIEWED`); web `ReplayPlayer` (MSE) and `RecordingPlayer` (chooses HLS or replay by the camera's server kind) mounted in the playback screen, the live recordings panel and the REC grid. Verified: unit tests (record log, offset, replay path, session cache, secrets, replay window), integration tests of the whole path with a fake recorder and a fake go2rtc (timeline, local-time source, stream removed at the end, refusals), 13 web tests, the full web suite (245 files), lint, and a real run against the unit through the Compose go2rtc: 26 segments in 6 hours; a 35 s motion segment replayed, main (copied) first picture in 2.0 s, sub (converted) in 4.8 s; the replay streams were removed on close. Known limits (documented): 1x only, no reverse, no shared clock across recorder cameras, no export of a clip.
- L4 (2026-10-28, owner): "no se pudo reproducir la grabacion tira en pantalla y en la timeline no tenemos nada, creo que tendriamos que mostrar la deteccioon de movimientos aunque sea si no tenemos analiticas". Diagnosis: the browser still ran the old bundle (PWA update prompt not accepted; the API log showed HLS `/vod` requests and no camera lookups); 35 s spans were sub-pixel on a day-wide bar; clicking the future part of the day built an inverted window (HLS 400). Fix (web only): spans are at least 2 px wide; a recorder camera's motion spans also become `motion` detections (`recorderMotion`, `useRecorderCameraIds`), shown as markers in Playback, the live recordings panel and the REC dock; `clampToNow` keeps a jump before now. Verified: unit tests for each, full web suite (250 files), typecheck, lint.
