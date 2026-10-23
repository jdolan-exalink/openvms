# Face embeddings and similarity suggestions

Frigate keeps detecting faces and cropping them. The OpenVMS worker embeds each retained crop and the
Train tab suggests pending faces that look like the same person. Suggestions never group or name anyone
by themselves: a manager confirms each grouping.

## Enable

1. Prepare the model (no weights ship in the image):
   `deploy/faces/prepare-arcface-model.sh` downloads the Open Model Zoo ArcFace ResNet100, checks its
   checksum and writes `arcfaceresnet100-8-spatial1.onnx` to `${OPENVMS_DATA_DIR}/models/faces`.
   The patch is required because the original export uses `BatchNormalization spatial=0`, which ONNX
   Runtime 1.29 rejects.
2. Restart the worker: `docker compose up -d worker`. Look for `face embedding ready` in its log.
   Without the file the worker logs `face embedding disabled` and the inbox works as before.

## Configuration (worker)

| Variable | Default | Meaning |
|---|---|---|
| `FACE_EMBEDDING_MODEL_PATH` | `/opt/openvms/face-models/arcfaceresnet100-8-spatial1.onnx` | CPU ONNX model |
| `FACE_EMBEDDING_HEF_PATH` | empty | Hailo-8 HEF; preferred over CPU when it initializes and passes the smoke inference |
| `FACE_EMBEDDING_MODEL_ID` | `omz-arcfaceresnet100-8-spatial1` | Stored with every vector, plus an input-preprocessing version (`+in2`). Change it with the model; vectors from other ids or preprocessing versions are recomputed and never compared |

## Notes

- Licence: the Apache-2.0 model card does not settle the training-data rights. The operator accepted that
  risk on 2026-10-23; revisit it before distributing the model.
- A crop that cannot be embedded is retried at most 5 times and fresh crops go first. Suggestions compare at most the 500 newest pending embeddings per request.
- Measured on the host CPU: about 100 ms per crop with 2 intra-op threads.
- The similarity threshold is `faces.DefaultSuggestionThreshold` (cosine 0.55). It was chosen by
  reviewing 49 real suggestion pairs after the input fix: same-person pairs held from 0.55 up and
  pairs below about 0.53 started mixing people. That sample covers only a few people, so it is not a
  calibration on consented data; treat suggestions as hints. Crops that are not faces (black frames,
  pavement) still match each other at any threshold; reject them from the Train tab.
- Crops are resized to 112x112 without landmark alignment, which lowers accuracy for tilted faces.
- Dismissing a suggestion in the UI lasts for the browser session only.
- Crops and vectors are biometric data: logs carry IDs and counts only.
- The OMZ ArcFace graph normalizes pixels itself and expects raw 0-255 RGB. Feeding it [-1, 1] (the first release, `in1`) made every crop embed almost identically (median cosine 0.986 across 76 real crops, so every suggestion read as a match). With raw bytes the same crops have median 0.18 and p90 0.37. The `+in2` suffix makes the worker recompute the old vectors on the next pass.
