#!/usr/bin/env bash
# Downloads the Open Model Zoo ArcFace ResNet100 ONNX model, verifies the published checksum and
# writes a copy whose BatchNormalization nodes use spatial=1. The original opset-8 export uses
# spatial=0, which ONNX Runtime 1.29+ rejects at inference time ("Invalid input scale").
# It also downloads the OpenCV Zoo YuNet face detector (checksum-verified), used to keep crops that
# are not faces out of the embeddings.
# Usage: deploy/faces/prepare-arcface-model.sh [target-dir]
set -euo pipefail

target="${1:-${OPENVMS_DATA_DIR:-/mnt/cctv/openvms}/models/faces}"
name="arcfaceresnet100-8.onnx"
url="https://storage.openvinotoolkit.org/repositories/open_model_zoo/public/2022.1/face-recognition-resnet100-arcface-onnx/${name}"
sha384="4bceb34ee96c7c0b544012be53870d757f332d9d2972962065618de6942cf60f63b94f702f85fcc31844b21de326ec72"

mkdir -p "$target"
if [ ! -f "$target/$name" ]; then
  curl -fSL --retry 3 -o "$target/$name.part" "$url"
  mv "$target/$name.part" "$target/$name"
fi
echo "$sha384  $target/$name" | sha384sum -c -

docker run --rm -v "$target:/m" python:3.12-slim sh -c '
  pip install -q onnx numpy >/dev/null 2>&1
  python - <<PY
import onnx
m = onnx.load("/m/'"$name"'")
n = 0
for node in m.graph.node:
    if node.op_type == "BatchNormalization":
        for a in node.attribute:
            if a.name == "spatial":
                a.i = 1
                n += 1
onnx.save(m, "/m/arcfaceresnet100-8-spatial1.onnx")
print("patched", n, "BatchNormalization nodes")
PY'
echo "Model ready: $target/arcfaceresnet100-8-spatial1.onnx"

detector="face_detection_yunet_2023mar.onnx"
detector_url="https://github.com/opencv/opencv_zoo/raw/main/models/face_detection_yunet/${detector}"
detector_sha256="8f2383e4dd3cfbb4553ea8718107fc0423210dc964f9f4280604804ed2552fa4"
# A file that is already there but fails the checksum (truncated, corrupted) is fetched again, and a
# download only takes its final name after it passes the checksum.
if [ -f "$target/$detector" ] && ! echo "$detector_sha256  $target/$detector" | sha256sum -c - >/dev/null 2>&1; then
  echo "Existing $detector fails its checksum; downloading it again" >&2
  rm -f "$target/$detector"
fi
if [ ! -f "$target/$detector" ]; then
  curl -fSL --retry 3 -o "$target/$detector.part" "$detector_url"
  if ! echo "$detector_sha256  $target/$detector.part" | sha256sum -c - >/dev/null 2>&1; then
    rm -f "$target/$detector.part"
    echo "Downloaded $detector does not match the pinned checksum" >&2
    exit 1
  fi
  mv "$target/$detector.part" "$target/$detector"
fi
echo "Detector ready: $target/$detector"
