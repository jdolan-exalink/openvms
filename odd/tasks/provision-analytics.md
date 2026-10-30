# Provision-ISR & NVR Analytics Integration (HTTP POST Webhooks)

Objective: extend OpenVMS Edge Agent and Central ingestion to accept, parse, and process real-time video analytics (line crossing, intrusion, human/vehicle detection, face detection, motion) pushed via HTTP POST from Provision-ISR / TVT cameras and NVRs, alongside existing LPR.

## Specs

- S1. The Edge Agent HTTP POST receiver accepts smart alarm events (`alarmStatusInfo`) from Provision-ISR cameras and NVRs without requiring a license plate string.
- S2. Normalized event types:
  - `lpr`: license plate detection (existing)
  - `line_crossing`: virtual line crossing / tripwire (`lineCrossAlarm`, `linecross`, `tripwire`)
  - `intrusion`: area / perimeter intrusion (`intrusionAlarm`, `intrusion`)
  - `vehicle`: vehicle presence / detection (`vehiceAlarm`, `vehicleAlarm`)
  - `human`: pedestrian / person presence (`humanAlarm`, `peopleAlarm`)
  - `face`: face detection (`faceAlarm`, `faceDetectionAlarm`)
  - `motion`: standard motion alarm (`motionAlarm`)
- S3. Target crop (`targetBase64Data`) and full scene (`sourceBase64Data`) are preserved across all analytic event types.
- S4. NVR multi-channel support: extract channel identity (`deviceNo.`, `chID`, `channelID`, `channel`) so central maps the event to the correct imported camera.
- S5. Central durable ingestion: `AgentIngestor` pulls normalized analytic events and forwards them into the alarm evaluation engine (`internal/alarms`) and events repository.
- S6. Backward compatibility: LPR ingestion, keepalives, and existing registration flows remain 100% functional.

## Slices

- [x] T1: Parser extension (`internal/agent/provisionlpr/parser.go`): parse `<alarmStatusInfo>`, smart event types, channel numbers, and images without plate requirement. (Strict TDD)
- [x] T2: Receiver & queue extension (`internal/agent/provisionlpr/`): accept non-plate analytics, deduplicate per type/channel, and store in durable event queue.
- [x] T3: Central ingestion & alarms (`internal/events/agent_ingest.go`): pull non-plate analytic events from agent, map NVR channels, and evaluate against alarm rules.
- [x] T4: Verification & live deployment to test site (helvecia1 running 0.4.6, central migration 86 applied).
