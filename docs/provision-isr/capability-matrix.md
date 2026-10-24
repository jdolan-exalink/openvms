# Provision-ISR capability matrix

One row per capability, with what is known about it and where that knowledge comes from. Nothing is
marked confirmed without evidence, and nothing is marked unsupported until a camera was tested and
did not do it.

- **Confirmed**: seen on a real camera or stated by the manufacturer's manual, with the source.
- **To verify**: expected or documented elsewhere, not yet seen on a real camera.
- **Unsupported**: tested on a real camera and it does not do it.

Sources: `DOC` is `docs/Provision_ISR_LPR_Integracion_Plugin.docx` (manufacturer manual, Smart Sight
Series, plus one lab capture), `CODE` is what OpenVMS implements today. Captures from a real camera
get an ID from `GET /v1/provision-lpr/captures` (see `capture.md`) and are listed in the Evidence
column when they exist.

Camera under test: I6-340 (named in the project brief). Exact model and firmware: **to verify** with
the ONVIF probe (the camera configuration screen shows both once probed).

## Transport and identity

| Capability | Status | Source and notes |
|---|---|---|
| Camera pushes `POST /SendKeepalive` (empty body, `application/xml`, expects HTTP 200) | Confirmed | DOC section 4, real capture from 10.0.15.107. Used as the online/offline heartbeat. |
| HTTP POST/API is configured in the camera web UI (Network > Advanced > HTTP POST, Protocol Type API, server address and port, heartbeat interval) | Confirmed | DOC sections 4 and 15. No API to set it remotely is documented. |
| Setting that push remotely through a CGI (`/http/post.cgi`) | To verify | CODE guessed it; DOC does not document it. Stage 1 removes the guess. |
| Setting that push over ONVIF | To verify | ONVIF defines no such setting; to confirm on the I6. |
| Stable camera identity inside the push (device ID, serial, MAC) | To verify | DOC section 17 lists it as pending. Until known, identity comes from the source IP plus the registered camera list. |
| Camera retry policy when the server does not answer 200 | To verify | DOC section 17. |
| HTTPS for the push | To verify | DOC section 13: do not assume it follows the admin HTTPS setting. |
| Long Polling Port (documented as 8080) for external software | To verify | DOC section 3. A pull alternative to the push; not investigated. |
| Auto Report Server (default port 2009) | Confirmed | DOC section 3. Reports the camera IP; it is not an LPR event. |

## License plate recognition

| Capability | Status | Source and notes |
|---|---|---|
| LPR runs inside the camera (plate to text, optional internal list, allow/block list) | Confirmed | DOC section 2 (manual). |
| Duplicate suppression in the camera, 5 to 120 s | Confirmed | DOC section 2. OpenVMS keeps its own 15 s window on top. |
| Panoramic snapshot and plate cutout as camera actions | Confirmed | DOC sections 2 and 10. |
| Endpoint, format and field names of the LPR reading in the push | To verify | DOC section 5: not documented and not captured. The parser in `internal/agent/provisionlpr` reads it heuristically; it must be replaced by what real captures show. |
| Confidence, direction and list status fields in the push | To verify | DOC section 17. |
| Panoramic and cutout images travel inside the push | To verify | DOC section 10: only confirmed as camera actions. |
| Vehicle attributes (color, brand, model, type, speed) in the push | To verify | The parser looks for them; no capture shows them. |

## Other analytics

The manufacturer document covers LPR only. Everything below is **to verify** on the real camera,
first through the ONVIF event topics it advertises (camera configuration screen, "Analíticas
anunciadas por la cámara"), then by seeing a real event. An advertised topic is not proof that the
camera fires it.

| Capability | Status | How it will be checked |
|---|---|---|
| Face detection | To verify | ONVIF topic, or a push event, plus a real capture. |
| Person detection | To verify | Same. |
| Vehicle detection and classification | To verify | Same. |
| Line crossing | To verify | Same. |
| Intrusion in an area | To verify | Same. |
| Motion and tamper | To verify | Same. |
| Person and vehicle attributes | To verify | Same. |
| Snapshots or metadata attached to those events | To verify | Same. |
| Manufacturer SDK or API | To verify | None found in the material reviewed. |

## ONVIF

| Capability | Status | Source and notes |
|---|---|---|
| Device information (manufacturer, model, firmware, serial) | Confirmed (OpenVMS side) | CODE: probe stores it in `cameras.onvif_capabilities`. The I6's values are still to be read. |
| Service list and profiles (S, T, M) | Confirmed (OpenVMS side) | CODE. The I6's services are to be read. |
| Event topics advertised (`GetEventProperties`), as full paths | Confirmed (OpenVMS side) | CODE, covered by tests with a specification-style response. The I6's topics are to be read. |
| Analytics detected from those topics | Confirmed (OpenVMS side) | CODE: lpr, face, person, vehicle, line_crossing, intrusion, motion, tamper, always marked advertised. |
| Receiving ONVIF events (PullPoint or notifications) | To verify | CODE has the client, used only for probing. Not an ingestion path yet. |
| Profile M metadata stream | To verify | Not implemented. |

## How to fill it in

1. Probe the camera from its configuration screen; record model, firmware, services and topics.
2. Set `OPENVMS_AGENT_PROVISION_CAPTURE=all` on the agent that sees the camera, point the camera at
   it, trigger each analytic, and read the captures.
3. For each row, change the status and put the capture IDs, firmware and date in the notes.
4. Turn the capture mode back to the default when the protocol is known.
