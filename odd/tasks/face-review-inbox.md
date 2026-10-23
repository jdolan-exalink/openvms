# Face review inbox and identity suggestions (FACE-07)

## Objective

Give operators one durable place to review every unclassified face attempt from authorized Frigate servers. Each unclassified identity has a stable visible ID. OpenVMS suggests when sightings may belong to the same person; an operator can confirm a group, assign it to a registered person, or create a new person.

The design must preserve the identity and sighting links needed for later cross-camera timelines and map views.

## Current behavior and gap

- `internal/faces.Service.ListTrainAttempts` fetches Frigate's `train` files at read time, caps the response per server and globally, and does not persist an attempt.
- `apps/web/src/components/faces/TrainTab.tsx` shows those recent files and already allows a manager to assign one file to an existing person or create a person.
- The list is keyed by server and filename. It has no stable identity ID, no grouping across servers/cameras, and no durable sighting history after Frigate removes an attempt.
- Registered persons already have central tenant-scoped IDs and their known sightings are listed from event `sub_labels`.
- The worker already uses ONNX Runtime for vehicle classification, but the repository has no face embedding or face similarity implementation.

## Product behavior

- Persist unclassified attempts centrally as sightings with a stable opaque ID, tenant, source server, event/camera when available, time, and protected image reference.
- Show unclassified identities and all their sightings to authorized `faces.view` users, including identities without a similarity match.
- Suggest groups by face similarity across cameras and servers. Suggestions never assign a person name automatically; a user confirms grouping or rejects it.
- A manager can assign an identity or confirmed group to an existing person, or create a person and assign it. Reuse the central person registry and its sync behavior.
- Keep identity history linked to the event, camera, site, and server so future maps and cross-camera tracking can use it without re-importing Frigate files.
- Use `faces.view` and `faces.manage`, tenant RLS, and audit behavior. Do not log face crops or embeddings.
- Keep Frigate's face detection and source workflow unchanged. Run embedding, comparison, and grouping in OpenVMS; select the inference profile from accelerators available to the OpenVMS worker, not from Coral/OpenVINO detected on each Frigate host.
- Detect CPU and Hailo-8 capability automatically at the OpenVMS worker. Prefer Hailo-8 when its runtime is usable, otherwise use CPU. Confirm the provider can initialize and run a smoke inference before selecting it; fall back safely when a device is visible but unusable. Defer NVIDIA acceleration.

## Work slices

- [x] FACE-07.1 — Persist and idempotently ingest Frigate unclassified attempts before Frigate retention removes them; resolve event/camera metadata when possible. Define cleanup and unavailable-server recovery.
- [ ] FACE-07.2 — Evaluate a self-hosted embedding model and preprocessing on authorized face-attempt crops. Reuse the existing ONNX Runtime path where it fits. Record model checksum, preprocessing, thresholds, latency, and model/weight license evidence before choosing a model.
- [ ] FACE-07.2a — Add a separately testable central-worker runtime selector that prefers usable Hailo-8, then CPU, and only reports ready after provider initialization and smoke inference. Defer NVIDIA provider work.
- [ ] FACE-07.2b — Implement and package the CPU ONNX embedding backend first; require a configured model artifact and smoke inference, with no bundled weights until model rights are cleared.
- [ ] FACE-07.2c — Add the Hailo-8 HEF/HailoRT backend and container device access after the CPU path is stable; verify on a host where Hailo is exposed.
- [ ] FACE-07.3 — Store tenant-isolated identity groups and reviewable similarity suggestions. A new unmatched sighting gets a visible stable ID; merging suggestions requires an explicit manager action and audit record.
- [ ] FACE-07.4 — Refactor the Faces review tab to show identities, groups, similarity suggestions, source sightings, and existing assign/create-person actions. Keep the registered-person page and watchlists backed by the central person ID.
- [ ] FACE-07.5 — Validate identity-to-sighting links for later map and cross-camera timeline use; map UI and unknown-face alarms remain out of scope for this change.

## Constraints and acceptance

- Strict TDD. Keep implementation in reviewable work units and use the established `stacked-to-main` PR chain.
- Every stored observation and suggestion is tenant scoped and protected by RLS and Faces permissions.
- Repeated polling cannot duplicate a sighting or change its stable ID. Frigate unavailability does not erase centrally stored observations.
- Every unclassified sighting is visible even when no candidate group reaches the suggestion threshold.
- Similarity suggestions expose confidence and source context but never silently change identity or person name.
- Assigning to an existing or new person moves the identity history under that central person ID and preserves the existing face-library sync behavior.
- Face crops and embeddings are treated as sensitive biometric data; logs contain IDs and counts only.
- Verify with synthetic and curated consented samples, integration tests, generation checks, and performance measurements. No live-server or map deployment acceptance is included.

## Open implementation decisions

- Which commercially suitable, self-hosted model/profile and aligned input pipeline meet camera quality and latency on CPU and Hailo-8.
- How model and preprocessing versions are recorded so embeddings from different profiles are never compared; rebuild or explicitly retain the previous active profile after a hardware change.
- How long the central inbox retains crops and vectors after assignment, rejection, or source removal.
- Whether low-confidence suggestions are visible in a separate review queue or mixed with the unclassified identities.

## Progress

- 2026-10-22: current Train tab and backend flow inspected; FACE-07 scope and slice boundaries recorded before implementation.
- 2026-10-22: initial model research found the existing ONNX Runtime integration and a small OpenVINO face re-identification candidate. The candidate expects a tight aligned crop and outputs a 256-value embedding; validate its weight provenance and accuracy on camera crops before selection.
- 2026-10-22: FACE-07.1 adds a worker-driven, tenant-RLS-protected `face_sightings` inbox with stable UUIDs, `(tenant, server, source_file)` idempotency, retained crop objects, retryable metadata when object storage fails, and ingestion audit rows. Source event/camera links remain unset because the filename format has not been verified against an authorized Frigate instance.
- 2026-10-22: hardware-selection boundary agreed: Frigate keeps detecting and providing crops; OpenVMS worker performs embeddings and grouping. Auto-select using the OpenVMS worker's usable CPU, NVIDIA, or Hailo-8 runtime, with CPU fallback. Per-Frigate Coral/OpenVINO does not select the OpenVMS embedding model.
- 2026-10-22: worker architecture review found CPU-only ONNX Runtime in the central worker image and no NVIDIA/Hailo device passthrough. NVIDIA acceleration is deferred; use Hailo-8 when available and CPU otherwise.
- 2026-10-23: runtime selection now tries Hailo-8 then CPU, and ignores NVIDIA until that provider is added. Added a CPU ONNX adapter for the evaluated ArcFace 112×112/512-vector contract, requiring deployment-supplied model and library paths; it normalizes vectors and uses a synthetic crop for smoke inference. No weights are bundled and worker wiring, model/weight rights, face alignment validation, consented-crop evaluation, and Hailo-8 support remain pending. Verification: `GOCACHE=/tmp/openvms-go-build-cache go test ./internal/faces -count=1`, `CGO_ENABLED=0 GOCACHE=/tmp/openvms-go-build-cache go test ./internal/faces -run '^TestSelectEmbeddingRuntime' -count=1`, and `GOCACHE=/tmp/openvms-go-build-cache go build -o /tmp/openvms-worker-check ./apps/worker` passed; a real model smoke run is unavailable because no approved model artifact is configured. Runtime deployment: N/A until model artifact and worker wiring are available. Rollback: revert CPU adapter and runtime policy files together.
