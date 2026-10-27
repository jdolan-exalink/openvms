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
