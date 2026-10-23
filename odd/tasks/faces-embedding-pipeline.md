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
- [ ] E4 — S7: compose env, docs, deploy check with real model if available. inline

## Log
- L1 (2026-10-23): "continuemoms con faces tenemos que dejar terminada la feacture completamente y detectando caras"
- L2 (2026-10-23): user chose option 1: use ArcFace OMZ and assume the licence risk; model path is deployment config.
