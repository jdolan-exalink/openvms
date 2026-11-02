# Proactive face recognition: unknown handling, live view and a face base like plates

## Header
Task document: `odd/tasks/faces-proactive-recognition.md`.
Objective: make face recognition more proactive, catch more faces and handle unknown people better, working like
license plates (a growing face base) plus a live view. Reference: the approved demo "Rostros OpenVMS", local copy at
`/tmp/claude-0/-root-openvms/d5b0993b-df5b-4114-ade9-b4f57cf7c445/scratchpad/faces-demo/index.html`.
Branch: `feat/faces-proactive-recognition`. Delivery: `ask-on-risk`; reviewable work units (about 400 changed lines each),
PRs per slice, merge only on the user's word. Test runner: `go test ./internal/faces/... ./internal/api/...`;
`cd apps/web && npx vitest run <paths>`; `npx tsc --noEmit`.

## Specs
- S1 "que sea mas proactiva, detecte mas": per-server detection sensitivity and a minimum face quality are editable
  from the Faces settings (they map to the existing Frigate face config), plus a rule that proposes a person after
  N appearances in M days.
- S2 "mejor manejo de desconocidos": unknown faces are grouped automatically by embedding similarity (operator
  grouping stays), ordered by recurrence, and a group can be named, joined to a known person, put on a watchlist or
  discarded as one action. Unnamed groups expire after a configurable retention.
- S3 "mas parecido a matriculas donde podamnos ir armando una base de caras": a person detail with timeline,
  an appearance history that also lists unknown faces, and CSV export.
- S4 "una vista mas en vivo": a live tab on the Faces page with the detections as they arrive and state filters.
- S5 "me encanta vamos con todo": the approved demo (tabs En vivo, Desconocidos, Personas, Historial, Reglas y ajustes) is the target design.
- S6 (assumption, from the research) unnamed faces are kept for a limited time and deleted automatically; the default
  is 30 days and the plazo is configurable. Argentina's Ley 25.326 was not verified.

## Tasks
- T1 (S2) Backend: automatic unknown groups (cluster proposals from embeddings) and recurrence, no schema change. Route: inline. Status: done (clusters.go, unknown_clusters.go, GET /faces/review-inbox/clusters; unit tests green, API tests green).
- T2 (S2, S6, S7) Retention: see `odd/tasks/retention-settings.md`. Status: in progress on branch `feat/retention-settings` (stacked on this one).
- T3 (S3, S4) Web data layer: the live feed and the history merge `recognitions` (known) with `review-inbox` (unknown sightings) and refetch on an interval; no new endpoint. Route: inline. Status: done (lib/faceFeed.ts, 5 tests).
- T4 (S2, S4, S5) Web: Faces page with the new tabs En vivo and Desconocidos. Route: inline. Status: done (LiveTab, UnknownsTab, lib/faceClusters.ts; the page opens on En vivo; full web suite 2386 tests green).
- T5 (S3, S1) Web: Personas detail, Historial, CSV, Reglas y ajustes. Route: inline. Status: partial. Done: CSV export of the recognition history (lib/faceExport.ts, formula-safe). Already existing and kept: per-person recent recognitions in the detail, per-server detection/recognition/unknown thresholds in Config. Pending: persisted recurrence rule (needs T2).
- T6 Docs, tests and PRs. Route: inline. Status: pending.

## Log
- L1 (2026-11-02, user): "quiero que trabajemosn el reconocimiento facial para que sea mas proactiva, detecte mas y tengamos mejor manejo de desconocidos, vamos a investigar como lo hace NX witness para tomar como ejemplo y quiero que armes una demom de la nuueva pagina de faces asi la veo antes, la idea es que el rreconocimiento facial trabaje mas parecido a matriculas donde podamnos ir armando una base de caras pero que tambien tengamos una vista mas en vivo y analiza que tendencias hay parra el reconocimiento facial"
- L2 (2026-11-02, user, after seeing the demo): "me encanta, vamos con todo"
- L3 (2026-11-02, mapping): today unknowns are `face_sightings` rows (`review_status='pending'`) with a retained crop; grouping is manual (`face_identity_groups`, `GroupReviewSightings`); suggestions compare pending identities at a fixed 0.50 cosine (`internal/faces/suggest.go`); the recognitions history only lists events carrying a person name; there is no live tab; thresholds (`DetectionThreshold`, `RecognitionThreshold`, `UnknownScore`) are per-server Frigate settings in `internal/inventory/face_config.go`.
- L4 (2026-11-02, decision): T3 changed from a new backend endpoint to a web-side merge of two existing endpoints; unknown sightings carry no camera (server and time only), so the history shows the server for them. Reason: smaller and reversible, and the same pattern the Live "Tótem Rostros" channel already uses.
- L5 (2026-11-02, user): "dale, armá la retención con 30 días, la retención de openvms tenemos que poder configurarla en configuración así que hay que pensar eso y ahí poder variar las retenciones y obviamente tener en cuenta retenciones de los servidores y espacio en el volumen de guardado"
- L6 (2026-11-02, user): "Para toda la plataforma, dale"
