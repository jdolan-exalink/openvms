# Camera video codec: show H.265 in Live and change it from camera settings

Helvecia's Cementerio streams H.265 while the rest stream H.264. Many browsers cannot play H.265
over MSE, so the tile looks broken and nothing says why. The codec has to be visible at a glance in
Live view, and changeable from the camera settings when the camera supports it over ONVIF.

Findings:
- go2rtc already reports the real codec of every stream (`GET /api/go2rtc/streams`, producers ->
  medias, e.g. `video, recvonly, H265`), with no ONVIF needed. That is the codec Frigate pulls and
  the one Live plays, so it is the source of truth for the badge.
- The ONVIF side exists in part: the agent has `/v1/onvif/video-config` (get/set) and the API has
  `operateCameraOnvifVideoConfig`, but both use the Media1 service. H.265 lives in Media2
  (Profile T), and there is no way yet to ask which encodings a camera supports.

Decisions:
- The codec is stored per camera, for the live (sub) and hq (main) streams, refreshed on sync and on
  the health poller's periodic capability refresh. Values are lowercase `h264` / `h265`; NULL when
  unknown (go2rtc unreachable, stream not producing yet).
- The Live badge shows the codec of the quality the tile is playing (sub in the grid, main when
  expanded) and only for H.265.
- Changing the codec ships as a second PR: it needs a Media2 client, an options query and
  validation against a real camera.

## Slices

- [x] 1. Backend: read stream codecs from go2rtc, store them per camera, refresh them, expose
      `live_codec` / `hq_codec` in the camera API.
- [x] 2. Web: "H.265" badge on Live tiles with an explanatory tooltip.
- [ ] 3. (separate PR) Camera settings: supported encodings over ONVIF (Media1 + Media2) and a
      change-to-H.264 action, enabled only when the camera supports it.
