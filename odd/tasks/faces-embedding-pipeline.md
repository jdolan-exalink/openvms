# Faces embedding pipeline and similarity suggestions (FACE-07.2/07.3/07.4 closure)
Objective: finish the faces feature end to end: the worker embeds retained Frigate face crops, OpenVMS suggests which pending sightings look like the same person, and operators review suggestions in the Train tab.
Branch: feat/faces-embedding-pipeline. Delivery: ask-on-risk, stacked-to-main. Runner: `go test ./internal/faces/...`, `pnpm --dir apps/web test -- --run`.

## Specs
- S1: "Keep Frigate's face detection and source workflow unchanged. Run embedding, comparison, and grouping in OpenVMS" (from face-review-inbox.md).
- S2: The worker selects the embedding runtime with `SelectEmbeddingRuntime` (Hailo-8, then CPU) and only embeds when a model artifact is configured; without one it logs once and does nothing.
- S3: Embeddings are stored per sighting with the profile and model id; vectors from different profile/model ids are never compared.
- S4: "Suggestions never assign a person name automatically; a user confirms grouping or rejects it." Suggestions expose confidence and source context.
- S5: "Face crops and embeddings are treated as sensitive biometric data; logs contain IDs and counts only." Tenant RLS and `faces.view` / `faces.manage` apply.
- S6: Every unclassified sighting stays visible even with no suggestion.
- S7: The deployment documents the model path variables; no weights are bundled (user accepted ArcFace OMZ licence risk, 2026-10-23).

## Tasks
- [x] E1 — S2,S3,S5: migration + queries + `Embedder` + worker loop + config. inline
- [x] E2 — S3,S4,S6: similarity suggestion service + API (openapi, regenerate). inline
- [x] E3 — S4,S6: Train tab shows suggestions with confirm (reuses grouping) / dismiss. inline
- [x] E4 — S7: compose env, docs, deploy check with real model if available. inline

## Log
- L1 (2026-10-23): "continuemoms con faces tenemos que dejar terminada la feacture completamente y detectando caras"
- L2 (2026-10-23): user chose option 1: use ArcFace OMZ and assume the licence risk; model path is deployment config.
- L3 (2026-10-23): evidence. Real OMZ ArcFace failed on ONNX Runtime 1.29 and Python ORT 1.31 ("Invalid input scale: NumDimensions() != 3", BatchNormalization spatial=0, opset 8). Patching 154 BN nodes to spatial=1 (deploy/faces/prepare-arcface-model.sh) gives 512-value vectors, deterministic (cosine 1.0), about 100 ms/crop on host CPU (TestCPUEmbeddingRealModel, skipped without env). Patched model sha384 9f2f6178e929...a952b. TDD note: E1/E2 tests were written together with the code, so RED was not observed for them; E3 RED was observed. Dismissing a suggestion is session-local (no persistence). Not done: threshold calibration on consented crops, landmark alignment, Hailo real inference, deployment.
- L4 (2026-10-23): UI check after deploy showed every suggestion at 96-100%. Pairwise cosine over 76 real crops: median 0.986 with [-1, 1] input, 0.18 with raw 0-255 (OMZ graph normalizes itself). Fixed on fix/faces-embedding-input-range; stored model id now carries `+in2` so old vectors are recomputed. The earlier smoke test only proved determinism, not discrimination; TestCPUEmbeddingRealModelSeparatesDifferentImages now pins it (RED observed: cosine 0.985).
- L5 (2026-10-23): user: "hacé el filtro de \"parece una cara\" antes de embeber". Raw ArcFace feature norm did not separate non-faces (they spread over norm 4-13), so the user chose YuNet (OpenCV Zoo, MIT). Branch feat/faces-detection-gate: migration 00071 `face_detected`, detector + embedder gate, worker env `FACE_DETECTOR_MODEL_PATH`. TDD note: detector and gate tests were written together with the code; no RED observed. The positive case (a real face passes) has no committed fixture because crops are biometric; it rests on the calibration run recorded in docs/operations/face-embeddings.md.
- L6 (2026-10-23): user: "dale, calibrá el umbral". No labelled data existed (0 persons, 0 groups). User chose to calibrate from operator decisions: migration 00072 `face_suggestion_decisions`, POST /api/v1/faces/review-inbox/suggestion-decisions, "No es la misma persona" now persisted and applied server-side, "Agrupar juntos" records a `same` verdict. Branch feat/faces-suggestion-dismissals. Calibration itself waits for real verdicts; decisions only cover pairs above the current threshold, so they can justify raising it, not lowering it. TDD note: tests were written together with the code; no RED observed.
- L7 (2026-10-23): native review of the dismissals branch (approved, 2 warnings). Fixed: verdicts are keyed by one sighting per side (stable across group merges) instead of identity IDs; the `same` verdict is stored before grouping and its failure is shown instead of swallowed; the list query error is checked where it happens. Also found that the earlier UI test never mocked the decisions endpoint, so it passed on a swallowed 404; fixed.
