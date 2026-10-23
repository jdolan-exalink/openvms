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
| `FACE_EMBEDDING_MODEL_ID` | `omz-arcfaceresnet100-8-spatial1` | Stored with every vector. Change it with the model; vectors from other ids are recomputed and never compared |

## Notes

- Licence: the Apache-2.0 model card does not settle the training-data rights. The operator accepted that
  risk on 2026-10-23; revisit it before distributing the model.
- Measured on the host CPU: about 100 ms per crop with 2 intra-op threads.
- The similarity threshold is `faces.DefaultSuggestionThreshold` (cosine 0.5). It is not calibrated on
  consented crops yet; treat suggestions as hints.
- Crops are resized to 112x112 without landmark alignment, which lowers accuracy for tilted faces.
- Dismissing a suggestion in the UI lasts for the browser session only.
- Crops and vectors are biometric data: logs carry IDs and counts only.
