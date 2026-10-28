# Recorded video from Provision-ISR recorders

Objective: the playback screen shows the recordings of a registered recorder's cameras. Builds on `odd/tasks/provision-nvr-live-video.md` (PR #277, the go2rtc container).
Branch: `feat/nvr-playback`. Delivery strategy: `ask-on-risk`. Runner: `go test ./internal/...`, `go test -tags integration`, `pnpm --dir apps/web test`.
Status: started 2026-10-28.

## Specs

- S1. "el live se ve bien, el grabado no": the recordings that live inside the recorder play in OpenVMS' playback screen, for the cameras of a registered Provision-ISR recorder.
- S2. The timeline comes from the recorder's own record log, through the same `listCameraRecordings` answer the Frigate cameras use, so the timeline code does not change.
- S3. Times: the recorder's log is in UTC, but its replay URL takes the recorder's local time. The offset is read from the device, never assumed.
- S4. The browser never receives the recorder's address or password. Every playback session has its own go2rtc stream, removed when the session ends.
- S5. Honest limits: playback at normal speed only (the recorder reports `ReversePlayback=false` and streams at 1x), seeking starts a new session at the chosen time, and no export or download for recorder cameras yet.
- S6. A recorder that is unreachable, a time without recordings, or a refused credential are reported with the same error codes the live viewer uses.

## Tasks

- [ ] T1 (S2, S3) `internal/provisionnvr`: record log (`queryChlRecLog`), clock offset (`queryTimeCfg`), replay path; fixtures. Route: inline.
- [ ] T2 (S2, S3, S4) `media`: recorder session cache, `Recordings` for recorder cameras, replay stream in go2rtc with cleanup. Route: inline.
- [ ] T3 (S1, S4, S6) Gateway websocket `GET /media/v1/cameras/{id}/playback`. Risk: item 2 (credentials) and a path shared with live. Route: inline.
- [ ] T4 (S1, S5) Web: playback screens use the replay player for recorder cameras; no speed control. Route: inline.
- [ ] T5 Docs, roadmap; verify on the real unit.

## Log

- L1 (2026-10-28, owner): "el live se ve bien, el grabado no".
- L2 (2026-10-28): evidence from the real unit, read-only. ONVIF: the recorder lists `Record_<ch>_<stream>` recordings, `GetRecordingSummary` says data from 2026-04-07 to now, Replay reports `RTP_RTSP_TCP=true` and `ReversePlayback=false`, Search has no event or metadata search. `GetReplayUri` returns the format `rtsp://host:554/chID=<n>&date=YYYY-MM-DD&time=HH:MM:SS&timelen=<seconds>&streamType=main|sub&linkType=tcp`. The NVMS web API, from the device's own playback script, offers `queryDatesExistRec` (days with recordings), `queryChlsExistRec` and `queryChlRecLog` (segments: type, start, end, size; `recList timeZone="UTC"`). `queryTimeCfg` says timezone `ART3`, no daylight saving, and the current local time. Test with a known 37 s motion segment of channel 7 (07:27:41 local, 10:27:41 UTC): the replay URL with the **local** time delivered 36.4 s of video at 25 fps at about 1x speed; with the UTC time it delivered nothing and hung. In the last 3 hours the log held only MOTION segments, so the timeline is made of motion segments on this unit.
