# Vision AI adjustments

Follow-up to the Eventos side bar (`vision-events-panel`), from using it:

- The event popup is too small to see the photo and the photo cannot be zoomed.
- The toggle that opens the events bar belongs on the left.
- "Pantalla completa" only fills the page; it should go full screen like F11.
- LPR mode lists only what the ranked slots happened to carry (max 30, in memory). It should list the
  full plate detections and open the vehicle photo when a plate is clicked.
- The on-video plate list always shows 6 rows; the count has to follow the height available.

Decisions: the events bar opens on the left, next to its toggle. Full screen uses the Fullscreen API
and falls back to the old page-filling mode when the browser refuses it. The LPR mode reads the same
plate reads the LPR tab uses, so both show the same detections.

## Slices

- [x] 1. Event popup: larger, wheel zoom (and drag to pan, double click to reset) on the photo.
- [x] 2. Events toggle and bar on the left; real full screen.
- [x] 3. On-video plate list: rows follow the available height.
- [x] 4. LPR mode: full list of plate reads; clicking a plate opens the vehicle photo popup.
