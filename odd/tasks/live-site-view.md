# Live view: whole-site view, server selector, icon-only detection labels

Three small changes asked for in Live view:

1. Dragging a whole **site** from the explorer fills the grid with every camera of every server of
   that site, the way dragging a server or a folder already does.
2. A **server selector** (drop-down with a check per server) in the Live toolbar. A server is
   checked while any of its cameras is in the grid; unchecking removes its cameras from the grid and
   checking adds them back (growing the layout when needed). It is how a site view is trimmed to
   the servers wanted.
3. The labels that say what was detected inside a video stream show **only the icon**. The object
   name and the confidence move to the tooltip and the accessible name so nothing is lost.

Decisions:
- Item 2 changes the grid contents rather than hiding tiles, so it works with saved views, layouts
  and the existing add/remove paths, and it needs no extra persisted state.
- Item 3 applies where the label sits over the video: the Live Vision AI tile and the Vision AI
  tiles. The event popup and the events side bar are not over a stream and keep their text.

## Slices

- [x] 1. Detection labels over video are icon-only (tooltip and accessible name keep the text).
- [x] 2. Dragging a site fills the grid with every camera of its servers.
- [x] 3. Server selector with checks in the Live toolbar.
