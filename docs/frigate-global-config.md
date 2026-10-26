# Global Frigate configuration

Path: Configuración → Servidores → Editar servidor → Frigate → "Abrir editor". It is separate from the per-camera editor and edits the server's `config.yml` through the same raw read / diff / save (optional restart) / revision history flow.

| Tab | Frigate sections | Notes |
|---|---|---|
| Detectores IA | `detectors` | presets from the hardware the edge agent reports |
| Modelo | `model` | path, type, size, labelmap; counts of labels the model supports |
| Objetos disponibles | `objects` | read-only wide catalog with `supported` flags (from `objectCatalogQuery`), plus the global tracked list. Cameras pick a simplified subset; enabling a detection on one camera never edits the model |
| MQTT & OpenVMS | `mqtt` | |
| Grabación & Almacenamiento | `record`, `snapshots`, `database` | retention calculator |
| Aceleración | `ffmpeg` | `hwaccel_args` preset |
| Defaults de cámaras | `detect`, `motion`, `review`, `live`, `audio`, `timestamp_style`, `ui` (+ read-only view of `record`, `snapshots`, `objects`) | level 1 of the inheritance model |
| Birdseye | `birdseye` | |
| Otros parámetros | every other top-level key | nothing is lost; `cameras` are edited per camera |
| Editor YAML (avanzado) | whole file | |

Group ownership lives in `apps/web/src/lib/frigateGroups.ts`; a key belongs to exactly one group. Without `servers.manage` the screen is read-only (no save, no acceleration change). The camera-side "heredado del servidor" hints are not implemented: they need the profile level (U9).
