# Camera editor: PTZ and Fisheye tabs

Both tabs appear only when the camera has the capability (PTZ: ONVIF `has_ptz` or the `ptz` tag;
Fisheye: the `fisheye` / `ojo_de_pez` tag). Controls exist only for what the current path really does.

## PTZ

Control order: 1. ONVIF PTZ, 2. manufacturer driver/API, 3. specific adapter. Only ONVIF PTZ is wired.

| Function | State |
|---|---|
| Move, zoom, stop, speed | Works through `POST /api/v1/cameras/{id}/onvif/ptz` (`continuous_move`, `stop`). Press starts, release stops. Speed is a 0.1..1 fraction. Needs `cameras.manage`. |
| Presets: list, go | Works (`get_presets`, `goto_preset`). |
| Presets: create, rename, delete; home position | Disabled in the UI. `internal/onvif` has `SetPreset` (rename is `SetPreset` with the existing token), `RemovePreset`, `GotoHomePosition`, `SetHomePosition` and `GetNodes`, but the edge agent endpoint (`internal/agent/onvifdiscover`) only dispatches four actions, so nothing can reach them yet. |
| Focus | Disabled: ONVIF focus lives in the Imaging service, not PTZ. |
| Limits | Disabled: not exposed over ONVIF PTZ here. |
| Tours | Stored per camera (table `camera_ptz_tours`, API `/api/v1/cameras/{id}/ptz/tours`): name, steps (preset, dwell seconds, speed), repeat count (0 = until stopped). `cameras.view` lists, `cameras.manage` writes; rows are tenant-isolated by RLS. There is no runner: tours are stored, not executed. |
| Test from the preview | While the PTZ tab is open (and the user can manage the camera) the preview shows a compact pad. |

Follow-up to enable preset edit and home: add `set_preset`, `remove_preset`, `goto_home`, `set_home`
and `get_nodes` to the agent PTZ handler (agent version bump), then enable the buttons by node
capabilities (`MaximumPresets`, `HomeSupported`, `FixedHomePosition`). A tour runner (server side,
cancelable) would also go through that agent path.

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
