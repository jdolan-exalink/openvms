# Faces embedding pipeline and similarity suggestions (FACE-07.2/07.3/07.4 closure)
Objective: finish the faces feature end to end: the worker embeds retained Frigate face crops, OpenVMS suggests which pending sightings look like the same person, and operators review suggestions in the Train tab.
Branches (all merged to main): feat/faces-embedding-pipeline (#209), fix/faces-embedding-input-range (#210), feat/faces-bulk-reject (#211), fix/faces-suggestion-threshold (#212), feat/faces-detection-gate (#213), feat/faces-suggestion-dismissals (#214). Delivery: ask-on-risk, one PR per work unit. Runner: `go test ./internal/faces/...`, `pnpm --dir apps/web test -- --run`.

## Specs
- S1: "Keep Frigate's face detection and source workflow unchanged. Run embedding, comparison, and grouping in OpenVMS" (from face-review-inbox.md).
- S2: The worker selects the embedding runtime with `SelectEmbeddingRuntime` (Hailo-8, then CPU) and only embeds when a model artifact is configured; without one it logs once and does nothing.
- S3: Embeddings are stored per sighting with the profile and model id; vectors from different profile/model ids are never compared.
- S4: "Suggestions never assign a person name automatically; a user confirms grouping or rejects it." Suggestions expose confidence and source context.
- S5: "Face crops and embeddings are treated as sensitive biometric data; logs contain IDs and counts only." Tenant RLS and `faces.view` / `faces.manage` apply.
- S6: Every unclassified sighting stays visible even with no suggestion.
- S7: The deployment documents the model path variables; no weights are bundled (user accepted ArcFace OMZ licence risk, 2026-10-23).
- S8: "hacé el filtro de \"parece una cara\" antes de embeber" (L5). A crop the detector rejects gets no vector and no suggestions but stays in the inbox (S6).
- S9: "quiero que podamos borrar selecciones porque no son caras o porque no nos interesan ya que veo detecciones que no tienen caras" (L8).
- S10: "dale, calibrá el umbral" (L6). Assumption: calibration needs labelled pairs; the operator chose to collect them from their own decisions.

## Tasks
- [x] E1 — S2,S3,S5: migration + queries + `Embedder` + worker loop + config. inline
- [x] E2 — S3,S4,S6: similarity suggestion service + API (openapi, regenerate). inline
- [x] E3 — S4,S6: Train tab shows suggestions with confirm (reuses grouping) / dismiss. inline
- [x] E4 — S7: compose env, docs, deploy check with real model if available. inline
- [x] E5 — S4: ArcFace input fix (raw 0-255), `+in2` stored model id so old vectors recompute. inline. #210, commit ebdfee0
- [x] E6 — S9: "Rechazar seleccionados" in the Train tab. inline. #211, commit 4d0506c
- [x] E7 — S4: suggestion threshold 0.5 -> 0.55 from reviewed real pairs. inline. #212
- [x] E8 — S8,S6: YuNet detector gate (migration 00071, `face_detected`, worker `FACE_DETECTOR_MODEL_PATH`). inline. #213, commits 0aa1fa4, 86cf8de, 887ec91
- [x] E9 — S10: persisted suggestion verdicts (migration 00072, keyed by sighting pair), UI records `same`/`different`. inline. #214, commits 587c120, 127f9bf, 53b48cd, d269c6f
- [ ] E10 — S10: calibrate the threshold from `face_suggestion_decisions` once enough operator verdicts exist (query in docs/operations/face-embeddings.md). Blocked on data.

## Log
- L1 (2026-10-23): "continuemoms con faces tenemos que dejar terminada la feacture completamente y detectando caras"
- L2 (2026-10-23): user chose option 1: use ArcFace OMZ and assume the licence risk; model path is deployment config.
- L3 (2026-10-23): evidence. Real OMZ ArcFace failed on ONNX Runtime 1.29 and Python ORT 1.31 ("Invalid input scale: NumDimensions() != 3", BatchNormalization spatial=0, opset 8). Patching 154 BN nodes to spatial=1 (deploy/faces/prepare-arcface-model.sh) gives 512-value vectors, deterministic (cosine 1.0), about 100 ms/crop on host CPU (TestCPUEmbeddingRealModel, skipped without env). Patched model sha384 9f2f6178e929...a952b. TDD note: E1/E2 tests were written together with the code, so RED was not observed for them; E3 RED was observed. Dismissing a suggestion is session-local (no persistence). Not done: threshold calibration on consented crops, landmark alignment, Hailo real inference, deployment.
- L4 (2026-10-23): UI check after deploy showed every suggestion at 96-100%. Pairwise cosine over 76 real crops: median 0.986 with [-1, 1] input, 0.18 with raw 0-255 (OMZ graph normalizes itself). Fixed on fix/faces-embedding-input-range; stored model id now carries `+in2` so old vectors are recomputed. The earlier smoke test only proved determinism, not discrimination; TestCPUEmbeddingRealModelSeparatesDifferentImages now pins it (RED observed: cosine 0.985).
- L5 (2026-10-23): user: "hacé el filtro de \"parece una cara\" antes de embeber". Raw ArcFace feature norm did not separate non-faces (they spread over norm 4-13), so the user chose YuNet (OpenCV Zoo, MIT). Branch feat/faces-detection-gate: migration 00071 `face_detected`, detector + embedder gate, worker env `FACE_DETECTOR_MODEL_PATH`. TDD note: detector and gate tests were written together with the code; no RED observed. The positive case (a real face passes) has no committed fixture because crops are biometric; it rests on the calibration run recorded in docs/operations/face-embeddings.md.
- L6 (2026-10-23): user: "dale, calibrá el umbral". No labelled data existed (0 persons, 0 groups). User chose to calibrate from operator decisions: migration 00072 `face_suggestion_decisions`, POST /api/v1/faces/review-inbox/suggestion-decisions, "No es la misma persona" now persisted and applied server-side, "Agrupar juntos" records a `same` verdict. Branch feat/faces-suggestion-dismissals. Calibration itself waits for real verdicts; decisions only cover pairs above the current threshold, so they can justify raising it, not lowering it. TDD note: tests were written together with the code; no RED observed.
- L7 (2026-10-23): native review of the dismissals branch (approved, 2 warnings). Fixed: verdicts are keyed by one sighting per side (stable across group merges) instead of identity IDs; the `same` verdict is stored before grouping and its failure is shown instead of swallowed; the list query error is checked where it happens. Also found that the earlier UI test never mocked the decisions endpoint, so it passed on a swallowed 404; fixed.
- L8 (2026-10-23): user: "quiero que podamos borrar selecciones porque no son caras o porque no nos interesan ya que veo detecciones que no tienen caras". Done as E6 on the existing per-sighting reject endpoint (no API change).
- L9 (2026-10-23): deployed main ef4bc9d (migrations 70 -> 72). Production check by UI and database: 77 crops judged, 30 rejected as non-faces, 47 embedded; suggestions 82 (threshold 0.5, [-1,1] input) -> 52 (0.55) -> 8 (gate) -> 6 (after the first `same`). The 4 unique suggestion pairs left were the same person by eye. UI flow tested for real: "No es la misma persona" persisted across a full reload and hid the pair both ways; "Agrupar juntos" stored a `same` verdict (0.916) and a group. The test `different` row was deleted afterwards because the pair was the same person.
- L10 (2026-10-23): what this feature taught, for the next one:
  - A smoke test that proves determinism proves nothing about discrimination. The ArcFace [-1,1] input bug shipped because only "same crop twice gives cosine 1" was checked; the first look at real data (every suggestion 96-100%) exposed it. Check that unrelated inputs score low before trusting a model.
  - Calibrate on the real crops, by eye, before choosing thresholds: raw ArcFace feature norm did not separate non-faces (they spread over norm 4-13); YuNet at a 160 px placement did (junk <= 0.09, faces >= 0.20). Placement size mattered more than the score threshold.
  - Cheap-first experiments saved a model dependency decision: measure the free signal before asking for a new weights file.
  - A decision keyed by an ID that changes (identity/group IDs change on merge) silently rots; key labels by the stable thing (sighting IDs).
  - A swallowed error hid a test that never ran: the first suggestion-verdict UI test never mocked the endpoint and passed on a 404 nobody saw. Surface errors, then fix the tests that were passing for the wrong reason.
  - Labels recorded only above the threshold can justify raising it, never lowering it.
  - Operating notes: the first `make deploy` failed once in `git pull --ff-only` ("Cannot fast-forward to multiple branches") with nothing changed, and passed on retry. The host needs `/mnt/cctv/openvms/models/faces/face_detection_yunet_2023mar.onnx` (the prepare script downloads it); without it the worker keeps embedding without the gate, with a warning, and needs a restart.
- L11 (2026-10-23): follow-ups, none started: calibrate the threshold on operator verdicts (E10); landmark alignment before embedding; real Hailo-8 inference (FACE-07.2c); OpenVINO provider decision (FACE-07.2e, branch feat/faces-openvino-runtime, unpushed); infrared profile faces are rejected by the detector (score about 0.08); native-review leftovers: dismiss state split between handler and mutation (TrainTab), nested indexOf in the ordering assertion.
