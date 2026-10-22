# Live detection overlays: LPR only where there are reads, plus faces

Today every camera flagged LPR shows the plate list over its video, even with nothing to list
("Esperando patentes"). The list should appear only on cameras that actually have detections, and
faces need the same kind of panel. A camera that has both shows them as tabs.

Decisions:
- "Has detections" means the camera has at least one plate read (or face event) in what the overlay
  already fetches; while loading, or on error, nothing is drawn.
- Cameras carry an `lpr` flag but no face flag, so faces are data-driven: one shared query of the
  latest face events (not one per tile), grouped by camera, enabled only with `events.view`.
- One overlay component for both kinds. With both, a tab bar picks the list; the default is the kind
  with the newest detection. Collapse state stays shared.
- The same rule applies in Live, saved Vision AI tiles and the Vision AI page.

## Slices

- [x] 1. Overlay renamed to the detections overlay; it renders nothing until the camera has reads.
- [x] 2. Face detections: shared per-camera face query and face rows.
- [x] 3. Tabs when a camera has both; wire faces into Live, Live Vision AI tiles and Vision AI.
