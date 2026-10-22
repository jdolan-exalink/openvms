# Change a camera's video codec over ONVIF (slice 3 of camera-video-codec)

Live view now flags H.265 cameras (`camera-video-codec.md`). This is the other half: change the
codec from the camera settings when the camera supports it.

Findings:
- The existing `/v1/onvif/video-config` agent endpoint and `operateCameraOnvifVideoConfig` API use
  the ONVIF Media1 service and hand the *device* service URL to the media client. H.265 is only
  exposed by Media2 (Profile T), and nothing asks a camera which encodings it supports.
- The probe already discovers each service's own XAddr through GetServices/GetCapabilities.

Design:
- `internal/onvif/media2.go`: a Media2 client (get encoder configurations, get options for a
  configuration, set a configuration) and `PlanEncodingChange`, a pure function that turns the
  current configuration plus the camera's declared options into the configuration to send:
  resolution, profile, frame rate, GOV length, bitrate and quality are kept when the target
  encoding allows them and moved to the nearest allowed value otherwise. A target the camera does not
  list is refused before anything is written.
- The Media2 `Multicast` block is not re-sent (optional in the schema); streaming is unicast RTSP.
- Agent: `POST /v1/onvif/video-encoding` with `options` (read only) and `set` (mutating), each
  resolving the Media2 (else Media1) endpoint from the camera's own services.
- Server: provision service + API `operateCameraOnvifVideoEncoding`, same permissions as the other
  ONVIF operations (servers.manage + servers.config.secrets). The stored codec refreshes on the next
  health poll once go2rtc reconnects.
- Web: a "Códec de video" section in the camera settings; the change action is offered only for an
  encoding the camera listed, behind a confirmation (the stream drops for a few seconds).

Not verified against a real camera: the SOAP fixtures follow the ONVIF Media2 specification. The
first real change must be done on one camera, with the owner's explicit go-ahead.

## Slices

- [x] 1. Media2 client + `PlanEncodingChange` (pure, fixture-tested).
- [ ] 2. Agent endpoint (options / set) with service discovery.
- [ ] 3. Provision service + API + OpenAPI.
- [ ] 4. Camera settings UI.
