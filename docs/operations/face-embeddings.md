# Face embeddings and similarity suggestions

Frigate keeps detecting faces and cropping them. The OpenVMS worker embeds each retained crop and the
Train tab suggests pending faces that look like the same person. Suggestions never group or name anyone
by themselves: a manager confirms each grouping.

## Enable

1. Prepare the model (no weights ship in the image):
   `deploy/faces/prepare-arcface-model.sh` downloads the Open Model Zoo ArcFace ResNet100, checks its
   checksum and writes `arcfaceresnet100-8-spatial1.onnx` to `${OPENVMS_DATA_DIR}/models/faces`.
   The patch is required because the original export uses `BatchNormalization spatial=0`, which ONNX
   Runtime 1.29 rejects. The same script downloads the YuNet face detector
   (`face_detection_yunet_2023mar.onnx`, checksum-verified).
2. Restart the worker: `docker compose up -d worker`. Look for `face embedding ready` in its log.
   Without the file the worker logs `face embedding disabled` and the inbox works as before.

## Configuration (worker)

| Variable | Default | Meaning |
|---|---|---|
| `FACE_EMBEDDING_MODEL_PATH` | `/opt/openvms/face-models/arcfaceresnet100-8-spatial1.onnx` | CPU ONNX model |
| `FACE_EMBEDDING_HEF_PATH` | empty | Hailo-8 HEF; preferred over CPU when it initializes and passes the smoke inference |
| `FACE_DETECTOR_MODEL_PATH` | `/opt/openvms/face-models/face_detection_yunet_2023mar.onnx` | YuNet detector that runs before embedding. Set it empty to disable the gate. If the file does not exist yet (an upgrade that has not run the prepare script), the worker logs a warning, keeps embedding without the gate, and needs a restart once the file is there. If the file exists but cannot be loaded, embedding pauses (`face embedding paused` in the log) instead of embedding non-faces |
| `FACE_EMBEDDING_MODEL_ID` | `omz-arcfaceresnet100-8-spatial1` | Stored with every vector, plus an input-preprocessing version (`+in2`). Change it with the model; vectors from other ids or preprocessing versions are recomputed and never compared |

## Notes

- Licence: the Apache-2.0 model card does not settle the training-data rights. The operator accepted that
  risk on 2026-10-23; revisit it before distributing the model.
- A crop that cannot be embedded is retried at most 5 times and fresh crops go first. Suggestions compare at most the 500 newest pending embeddings per request, and a card shows at most 5 candidates, best first.
- Measured on the host CPU: about 100 ms per crop with 2 intra-op threads.
- The similarity threshold is `faces.DefaultSuggestionThreshold` (cosine 0.50). It was chosen on
  aligned embeddings by looking at the crops of the 28 pairs between 0.40 and 0.56: same-person
  pairs held down to about 0.46, the first clearly different people appeared at 0.44 (a person with
  white-framed glasses against a man in a red striped shirt), and two more unlikely matches at 0.43
  and 0.41. 0.50 keeps 0.06 of margin over the first error. The sample is about five people and the
  labels are from inspection, not operator verdicts, so it is not a calibration on consented data;
  treat suggestions as hints and re-check it against `face_suggestion_decisions` (see below). Crops
  that are not faces (black frames, pavement) match each other at any threshold, which is why the
  detector gate exists.
- With the detector gate on, each face is aligned before embedding: YuNet's five landmarks (eyes, nose, mouth corners) are mapped onto the standard ArcFace 112x112 template with a similarity transform. Without the gate, crops are stretched to 112x112 and tilted faces lose accuracy. The two inputs have different stored model versions (`+in3a` aligned, `+in2` stretched), so turning the gate on or off recomputes every vector. On 76 real crops, same-person pairs went from 0.59/0.67/0.58 to 0.68/0.77/0.86 (one near-duplicate pair went 0.92 -> 0.81) and the twelve highest-scoring unlabelled pairs were all the same person on inspection; unrelated pairs got a lower median (0.16 -> 0.06). Still not calibrated on consented data.
- "Not the same person" is stored (`face_suggestion_decisions`) and hides that pair from future suggestions, in both directions. "Group together" on a suggestion stores a `same` verdict. A verdict names one sighting on each side, so it keeps ruling out the two identities when groups merge later. Rows hold sighting IDs and the similarity the operator saw, never biometric data, and survive the crops they refer to. If a verdict cannot be stored the UI shows the error and does not group.
- Crops and vectors are biometric data: logs carry IDs and counts only.
- The OMZ ArcFace graph normalizes pixels itself and expects raw 0-255 RGB. Feeding it [-1, 1] (the first release, `in1`) made every crop embed almost identically (median cosine 0.986 across 76 real crops, so every suggestion read as a match). With raw bytes the same crops have median 0.18 and p90 0.37. The `+in2` suffix makes the worker recompute the old vectors on the next pass.
- Detector gate: each crop is checked once with YuNet before it is embedded. A rejected crop gets
  `face_detected = false`, loses any vector it had, never becomes a suggestion, and stays in the Train
  tab so a manager can still look at it or reject it. Enabling the gate also judges crops that were
  already embedded. The crop is scaled to 160 px and centered on YuNet's 640x640 canvas; the accept
  score is `faces.DefaultFaceDetectionScore` (0.15). On 76 real crops from one deployment, empty
  frames, pavement and cars scored at most 0.09 and every frontal face at least 0.20, but infrared
  profile faces score about 0.08 and are rejected, as are heavily cropped faces. That sample is not
  consented data. About 13 ms per crop. A verdict is final until `face_detected` is reset to NULL in
  the database; a detector failure is retried like an embedding failure.

## Calibrating the threshold

Operator verdicts are the calibration data. After a few weeks of use:

```sql
SELECT decision, count(*), round(min(similarity)::numeric, 3) AS min,
       round((percentile_cont(0.5) WITHIN GROUP (ORDER BY similarity))::numeric, 3) AS median,
       round(max(similarity)::numeric, 3) AS max
FROM face_suggestion_decisions GROUP BY decision;
```

Verdicts only exist for pairs at or above the current threshold, so they can justify raising it
(`different` verdicts piling up near the threshold) but cannot show same-person pairs that were never
suggested, so they cannot justify lowering it.
