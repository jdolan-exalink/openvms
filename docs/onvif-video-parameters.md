# ONVIF video parameters (building blocks)

Package `internal/onvif`. The UI talks about video (resolution, FPS, codec, bitrate, bitrate mode, GOP, quality), never about FFmpeg. Only what the device declares is offered.

| Function | Purpose |
|---|---|
| `VideoEncoderOptions(ctx, conn, profileToken)` | Per codec (H264/H265/MJPEG/MPEG4): resolutions, FPS (discrete list and range), bitrate, GOP, encoding interval, quality, codec profiles, bitrate modes. A missing item stays empty; it is never invented. |
| `ProfileStreams(ctx, conn)` | Current video settings per profile (token, name, codec, resolution, FPS, bitrate, mode, GOP, quality) and whether the profile has an audio encoder. |
| `ValidateVideoChange(options, desired)` | Pure. Returns every violated constraint as a typed `Violation` (`unsupported_codec`, `unsupported_resolution`, `fps_out_of_range`, `bitrate_out_of_range`, `gop_out_of_range`, `quality_out_of_range`, `unsupported_bitrate_mode`, `invalid_value`, `options_unavailable`). Limits the device did not report are not checked. Set `VideoDesired.CurrentCodec` so codec-dependent fields are checked against the codec in use when the codec is not changed. |
| `ClassifyImpact(current, desired)` | `stream_restart` for a real change of codec, resolution, FPS, bitrate or GOP, with the message "Este cambio reiniciará el stream durante aproximadamente algunos segundos." Show it before saving. |
| `SetVideoEncoder(ctx, conn, profileToken, desired)` | Read-modify-write: reads the profile and the options, refuses unsupported changes (`*ValidationError`) without writing, sends the full configuration changing only the requested fields, then re-reads and returns `*VerifyError` if the device did not apply them. |

## Services

`Conn` carries the validated endpoints (`ParseEndpoint`; `NewConn` validates both). Media2 (Profile T) is used when its endpoint is set (the only service that exposes H.265, GOV length and constant bitrate); otherwise Media (ver10). On Media (ver10) the GOP is only writable for H.264 and bitrate mode is not reported.

## Safety

- Endpoints must be valid `http(s)` URLs without user info; the SOAP client revalidates them on every call. URLs returned by the device are never followed.
- Errors are the SOAP client's `*CallError` (status and code only): no fault text, URL or credential. The password only travels as a WS-Security digest.
- Mutating calls are never retried.

## Wiring into the camera configuration (phase B, backend)

`internal/camconfig` consumes the building blocks through the `VideoDevice` interface (`onvif.ConnDevice` implements it) and a `VideoLink` (device, reason code, and the ONVIF profile tokens of the primary and detection streams).

**Reaching the camera.** `GET /api/v1/cameras/{id}/video-options`, the review/apply of `streams.*` and the video values of `GET .../normalized-config` use the camera's stored ONVIF endpoint and the credentials resolved like the ONVIF probe does. Using them needs `servers.manage` and `servers.config.secrets` on the camera's server (`RequireServerManageAndConfigSecrets`). The model read degrades without the permission (no error); video-options, review and apply of a stream change answer 403. The connection (`onvif.DiscoverConn`) only accepts media services on the same host as the device endpoint and only dials private addresses (`onvif.PrivateNetworkTransport`: RFC 1918, ULA, 100.64/10). A camera that is not reachable from the API server (a LAN behind an edge agent) reads as `unreachable`: reaching it through the agent needs a new agent endpoint (follow-up).

**Which profile is which stream.** `camconfig.ProfileTokens` matches the stored probe result: the profile whose `stream_uri` equals `main_stream_url` is the primary stream, the one equal to `sub_stream_url` the detection stream. No match, an ambiguous match, or a single-stream camera leaves the token empty; nothing is guessed.

**Read.** The primary stream gets resolution, FPS, bitrate and GOP (and the codec when go2rtc did not report one) from the profile. The detection stream keeps Frigate's detect size and rate and only gains missing values. The `onvif` entry of `sources` is `ok` or `unavailable` with a reason code in `detail`.

**Reason codes** (`video-options`, `sources[onvif].detail`): `no_onvif` (no ONVIF endpoint), `no_credentials` (none stored, or no permission to use them), `unreachable` (the camera did not answer), `unsupported` (it does not expose the settings or the stream's profile is unknown). Error text from the device is never copied.

**Patch** (`streams.primary` / `streams.detection`, fields `resolution {width,height}`, `fps`, `codec`, `bitrate_kbps`, `gop`) and the path each field takes:

| Field | Primary | Detection with its own ONVIF profile | Detection without one |
|---|---|---|---|
| resolution, fps | ONVIF encoder | ONVIF encoder | Frigate `detect.width/height/fps` (fps must be an integer) |
| codec, bitrate_kbps, gop | ONVIF encoder | ONVIF encoder | rejected (`unsupported` item) |

Review reads the device (read only), runs `ValidateVideoChange` (each violation is an `unsupported` item such as "Stream principal: La resolución 3840x2160 no está soportada con H264.") and `ClassifyImpact` (impact `stream_restart`, "Este cambio reiniciará el stream durante aproximadamente algunos segundos."). Apply runs through the desired/effective pipeline: the desired state is recorded first, the camera is written before Frigate (`SetVideoEncoder`, read-modify-write and verification) so a refusal leaves everything else untouched, and the outcome and audit row carry old/new values and origin. A refused or unverified write ends in `sync_state: error` with a sanitized `last_error`. Stream fields are not part of the drift comparison: the verification is the one `SetVideoEncoder` does against the device.
