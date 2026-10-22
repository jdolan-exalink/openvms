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

## Work slices

- [x] FACE-07.1 — Persist and idempotently ingest Frigate unclassified attempts before Frigate retention removes them; resolve event/camera metadata when possible. Define cleanup and unavailable-server recovery.
- [ ] FACE-07.2 — Evaluate a self-hosted embedding model and preprocessing on authorized face-attempt crops. Reuse the existing ONNX Runtime path where it fits. Record model checksum, preprocessing, thresholds, latency, and model/weight license evidence before choosing a model.
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

- Which commercially suitable, self-hosted embedding model and aligned input pipeline meet the expected camera quality and latency.
- How long the central inbox retains crops and vectors after assignment, rejection, or source removal.
- Whether low-confidence suggestions are visible in a separate review queue or mixed with the unclassified identities.

## Progress

- 2026-10-22: current Train tab and backend flow inspected; FACE-07 scope and slice boundaries recorded before implementation.
- 2026-10-22: initial model research found the existing ONNX Runtime integration and a small OpenVINO face re-identification candidate. The candidate expects a tight aligned crop and outputs a 256-value embedding; validate its weight provenance and accuracy on camera crops before selection.
- 2026-10-22: FACE-07.1 adds a worker-driven, tenant-RLS-protected `face_sightings` inbox with stable UUIDs, `(tenant, server, source_file)` idempotency, retained crop objects, retryable metadata when object storage fails, and ingestion audit rows. Source event/camera links remain unset because the filename format has not been verified against an authorized Frigate instance.
- 2026-10-22: hardware-selection boundary agreed: Frigate keeps detecting and providing crops; OpenVMS worker performs embeddings and grouping. Auto-select using the OpenVMS worker's usable CPU/NVIDIA/Hailo-8 runtime, with CPU fallback. Per-Frigate Coral/OpenVINO does not select the OpenVMS embedding model.
<<<<<<< HEAD
=======
- 2026-10-22: review API and UI slice adds tenant-scoped inbox listing, private crop reads, explicit assign-to-existing/create-person actions, and rejection with queued crop deletion. The Train tab now shows the stable UUID and retained image after Frigate removes its source file. FACE-07.4 remains open for identity groups and similarity suggestions; FACE-07.2/07.3 and map/timeline validation are not implemented.
- 2026-10-22: FACE-07.2 research identifies ArcFace ONNX as an evaluation candidate, not a selected model. FACE-07.3a will allow managers to group sightings explicitly while automated similarity remains disabled pending consented sample evaluation and a training-data rights decision.
- 2026-10-22: FACE-07.3a adds tenant-scoped manager grouping for 2–100 pending sightings with retained crops, stable group IDs on merge, audit records, and identity IDs in the inbox. Similarity suggestions remain disabled. Delivery: stacked PRs to main; slice 1 persistence/API, slice 2 dependent UI. Verification: `GOCACHE=/tmp/openvms-go-build-cache go test -tags integration ./internal/faces ./internal/api -run '^(TestReviewSightingsAssignAndRejectByStableID|TestReviewSightingsExplicitIdentityGroupsAndMerge|TestFacesAPIReviewInboxStableIDsAssignmentAndRejection|TestFacesAPIExplicitReviewIdentityGrouping)$' -count=1` passed; runtime deployment N/A. Rollback: revert migration 00068 and grouping service/API behavior together.
- 2026-10-22: slice 1 committed as `765872a`. Slice 2 adds manager multi-select and group display in the Train tab. Verification: `pnpm exec vitest run src/components/faces/TrainTab.test.tsx --reporter=dot` and `pnpm typecheck` passed; runtime deployment N/A. Rollback: revert the Train tab, locale keys, and generated TypeScript contract changes together.
- 2026-10-22: hardware-selection boundary agreed: Frigate keeps detecting and providing crops; OpenVMS worker performs embeddings and grouping. Auto-select using the OpenVMS worker's usable CPU/NVIDIA/Hailo-8 runtime, with CPU fallback. Per-Frigate Coral/OpenVINO does not select the OpenVMS embedding model.
>>>>>>> b874e40 (docs(faces): define centralized hardware selection)
