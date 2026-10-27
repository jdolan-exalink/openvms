# Camera editor: PTZ and Fisheye tabs

Both tabs appear only when the camera has the capability (PTZ: ONVIF `has_ptz` or the `ptz` tag;
Fisheye: the `fisheye` / `ojo_de_pez` tag). Controls exist only for what the current path really does.

## PTZ

Control order: 1. ONVIF PTZ, 2. manufacturer driver/API, 3. specific adapter. Only ONVIF PTZ is wired.

| Function | State |
|---|---|
| Move, zoom, stop, speed | Works through `POST /api/v1/cameras/{id}/onvif/ptz` (`continuous_move`, `stop`). Press starts, release stops. Speed is a 0.1..1 fraction. Needs `cameras.manage`. |
| Presets: list, go | Works (`get_presets`, `goto_preset`). |
| Presets: create, rename, delete; home position | Works with agent 0.4.5 or later: `set_preset` (create; `preset_name` optional; rename is `set_preset` with the existing `preset_token`, which also re-saves the position), `remove_preset`, `goto_home`, `set_home`, all needing `cameras.config.ptz` (legacy `cameras.manage` keeps it) and audited as ONVIF mutations. `get_presets` and `get_nodes` return `capabilities` read from ONVIF `GetNodes`: `preset_edit` (the node stores presets), `home` (a home position exists), `home_set` (home exists and is not fixed), `pan_tilt`, `zoom`, `focus` (always false). When they cannot be read, `known` is false and `reason` is `agent_outdated` (update the agent) or `unavailable`; the UI then disables the controls and says why. With several PTZ nodes the first one that stores presets decides. |
| Focus | Disabled: ONVIF focus lives in the Imaging service, not PTZ. |
| Limits | Disabled: not exposed over ONVIF PTZ here. |
| Tours | Stored per camera (table `camera_ptz_tours`, API `/api/v1/cameras/{id}/ptz/tours`): name, steps (preset, dwell seconds, speed), repeat count (0 = until stopped). `cameras.view` lists, `cameras.manage` writes; rows are tenant-isolated by RLS. |
| Tour runner | `POST .../ptz/tours/{tourId}/start` and `.../stop` (`cameras.config.ptz`); the tours GET carries the camera's `status` (`idle`, `running`, `finished`, `stopped`, `failed`, with step, round and stop reason). It goes to each preset with `goto_preset` through the same agent path, waits the dwell time (at least 1 s between steps) and repeats. One tour runs per camera (409 `tour_running`); it refuses to start without presets (409 `no_presets`), with a preset that no longer exists (409 `preset_missing`) or when the PTZ does not answer (502 `ptz_unavailable`). Any manual movement (`continuous_move`, `stop`, `goto_preset`, `goto_home`) cancels the running tour, as does editing or deleting it and an API shutdown (`Handlers.StopPTZTours`). The runner is in memory: restarting the API stops the tours and, with several API replicas, each one only knows the tours it started. The step `speed` is stored but not applied: `GotoPreset` carries no speed here. |
| Test from the preview | While the PTZ tab is open (and the user can manage the camera) the preview shows a compact pad. |

Restart: `POST /api/v1/cameras/{id}/restart` with `{"confirm": true}` sends ONVIF `SystemReboot` through the agent (operation `reboot`; direct private-network call when the server has no agent). It needs `cameras.restart` (legacy `servers.restart` holders keep it, nobody else gains it) and writes an audit event `CAMERA_RESTART` (user, camera, started and result).

## Fisheye

The tab wraps the existing fisheye section (lens type, mount, projection, calibration, 360 view,
virtual PTZ, 3x120 virtual views) and states the three things that must not be mixed up:

- Original stream: untouched.
- Dewarped view: a display setting applied over the original stream in the browser; it is stored in
  the browser (localStorage key `openvms.fisheye.config.<cameraId>`), never written to the camera,
  Frigate or the recording.
- Original recording: not changed by dewarping.

Not done: a stored per-camera "export original or dewarped" preference (the export pipeline has no
hook for it yet); the choice is meant to be made at export time. The 2x180 mode exists in the render
engine (`panorama`) but the existing section's mode selector does not list it.
