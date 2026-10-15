# Frigate Server Intelligence, AI Detectors, Birdseye and Storage Retention

## Objective

Enhance server and Frigate management in OpenVMS with:
1. Dynamic remote AI hardware detection (NVIDIA, Intel OpenVINO, Google Coral USB/PCIe, Hailo-8) via `edge-agent`.
2. Global tracked objects selector with icons and COCO/Frigate validation.
3. Server-level `BirdEye` stream integration into OpenVMS live grid.
4. Dynamic storage and recording retention calculator with 15% safety margin, per-camera priority allocation, edge-agent watchdog auto-prune, and sync of events, clips, thumbnails, and LPR plate crops.

## Current Baseline

- `internal/agent/sample.go` samples basic metrics, Coral (`/dev/apex_0` or USB `1a6e:089a` / `18d1:9302`), GPU (`hoststat`), CCTV and Database disk stats (`CCTVTotal`, `CCTVFree`, `DatabaseTotal`, `DatabaseFree`).
- `apps/web/src/components/servers/ServerFrigateConfigModal.tsx` has tabs for MQTT, Detectors (static presets), Objects (8 basic items, no icons), Record, Birdseye, YAML, and History.
- Frigate streams are accessed via go2rtc proxy; `birdseye` stream exists on Frigate servers at `rtsp://<server>:8554/birdseye` but is not registered as a first-class stream/channel in OpenVMS.
- Storage retention relies purely on Frigate's internal cycling; no dynamic days calculator, no camera priority weighting, and no fallback pruning when disk exceeds 85%.

## Tasks

- [x] FSI-01 — Remote AI hardware probe in `edge-agent`: detect NVIDIA (driver/PCI `10de`), Intel OpenVINO (renderD128/AVX), Google Coral (differentiating USB vs PCIe `/dev/apex_*`), and Hailo-8 (`/dev/hailo*` / PCI `1e60`). Expose in `Snapshot` and API server telemetry.
- [x] FSI-02 — UI Detectores IA in `ServerFrigateConfigModal`: display detected hardware badges per server and 1-click detector configuration presets (TensorRT, Coral USB, Coral PCIe, OpenVINO, Hailo-8, CPU fallback).
- [x] FSI-03 — UI Objetos Globales: complete COCO 80 / Frigate supported object catalog with Lucide icons, category grouping (Personas, Vehículos, Animales, Accesorios), visual pill selection, and YAML sync.
- [x] FSI-04 — Birdseye Stream Channel: auto-provision/link `BirdEye` stream per Frigate server into OpenVMS channel list and Live grid.
- [x] FSI-05 — Dynamic Storage & Retention Calculator: UI in Frigate Config calculating attainable recording days based on CCTV storage minus 15% safety buffer and camera stream bitrates, with per-camera priority sliders.
- [x] FSI-06 — Edge-Agent Storage Watchdog & Safety Pruning: background disk guard enforcing the 15% safety buffer on CCTV and DB mounts; emergency old segment pruning if Frigate fails to cycle and disk > 85%.
- [x] FSI-07 — Event & LPR Plate Crop Synchronization: sync Frigate events, snapshots, thumbnails, and license plate crops into OpenVMS backend and media viewer.

## Delivery Strategy

- Strategy: `ask-on-risk` (forecast > 1500 authored lines across agent, backend, and web UI).
- Slices:
  - Slice 1: FSI-01 + FSI-02 (AI Hardware Detection & Detector UI).
  - Slice 2: FSI-03 + FSI-04 (Global Objects with Icons + Birdseye Channel).
  - Slice 3: FSI-05 + FSI-06 (Storage Calculator, 15% Buffer & Watchdog Pruner).
  - Slice 4: FSI-07 (Events & LPR Crop Sync).
