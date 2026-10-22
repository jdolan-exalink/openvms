# Frigate detection stall detection

Helvecia's Frigate kept recording and streaming but stopped detecting (`detection_fps` = 0 on every
camera, detector alive), so OpenVMS received no events or plate reads and nothing flagged it. The
health poller already reads `/api/stats` per server and only used `camera_fps`.

Decision: detect it server-side in the health poller (the server owns the authenticated Frigate
client; the edge agent has no Frigate credentials and only reports host metrics).

Known limit: `detection_fps` is also 0 in a quiet scene, so the hold is long (30 min) and the
verdict needs every camera that delivers video to be at 0.

## Slices

- [x] 1. `DetectionTracker`: hold/recover state machine over per-server camera stats (pure, unit tested).
- [ ] 2. Health poller: a stalled server becomes `degraded` with a reason in `last_error` and
      `detection_stalled_since` in its stats; recovery clears it.
