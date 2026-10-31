# ODD Task: Provision LPR & Analytics Completion

## 1. Overview
Complete the remaining capabilities for Provision-ISR / TVT cameras in Edge Agent and Central Ingestion:
1. **LPR Coordinates & List Status**: Parse bounding box coordinates (`plate_box`) and camera list status (`sub_label`) from XML and JSON payloads into `lpr_reads`.
2. **Face Recognition & Sighting Integration**: Extract face detection metadata (`person_name`, `face_score`) and ingest face crops into the Face Review Inbox (`face_sightings`) and `events.sub_labels`.
3. **People Counting Analytics**: Parse flow/counter alarms (`people_counting`, `target_count`) with entering, exiting, and passing totals, routing them through central event ingestion.

## 2. Work Units & Checklist

### Slice 1: LPR Bounding Box Coordinates & List Status
- [x] Extend `LPREvent` struct with `PlateBox []float32` in `internal/agent/provisionlpr/types.go`.
- [x] Implement XML & JSON bounding box parsing in `internal/agent/provisionlpr/parser.go` (`<rect>`, `<plate_rect>`, `<location>`, `"rect"`, `"bbox"`).
- [x] Unit tests for coordinate parsing and normalization in `internal/agent/provisionlpr/parser_test.go`.
- [x] Ingest `plate_box` and camera `list_status` into `lpr_reads.plate_box` and `lpr_reads.sub_label` in `internal/events/agent_ingest.go`.
- [x] Integration test in `internal/events/agent_ingest_test.go`.

### Slice 2: Face Analytics Ingestion & Face Review
- [x] Extend `LPREvent` with `PersonName *string` and `FaceScore *float64` in `internal/agent/provisionlpr/types.go`.
- [x] Parse face identification fields in `internal/agent/provisionlpr/parser.go` (`<personName>`, `<name>`, `<similarity>`, `<faceID>`).
- [x] In `internal/events/agent_ingest.go`:
  - When `eventType == "face"`: populate `events.sub_labels` with `PersonName`.
  - When face image/crop is present: insert a pending row in `face_sightings` with `sightingSystemActor`.
- [x] Unit and integration tests in `parser_test.go` and `agent_ingest_test.go`.

### Slice 3: People Counting Analytics
- [x] Recognize `people_counting` alarm keys in `internal/agent/provisionlpr/parser.go` (`peoplecount`, `targetcount`, `passcount`, `entercount`, `exitcount`, TVT plate `"COUNT"`).
- [x] Extract counts (`EnterCount`, `ExitCount`, `PassCount`) in `LPREvent`.
- [x] Map `people_counting` in `internal/events/agent_ingest.go` to labels `["people_counting", "count"]` with severity `detection`.
- [x] Tests for people counting parsing and ingestion.

### Slice 4: ONVIF PullPoint Event Subscription & Polling
- [x] Add `POST /v1/onvif/pull-events` in `internal/agent/onvifdiscover/ops.go` to create PullPoint subscription and pull messages.
- [x] Wire route in `apps/edge-agent/main.go` under `tokens.ScopeControl`.
- [x] Unit and integration tests in `internal/agent/onvifdiscover/ops_test.go` and `apps/edge-agent/main_test.go`.
