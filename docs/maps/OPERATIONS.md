# Make Maps operational

A basemap is not a commissioned camera map. Select an authorized site, verify its camera
inventory, and save geographic placements before expecting camera markers. This guide covers
the operational completion follow-up. Final local web tests/typecheck/lint/build, ordinary and
integration Go race checks, and the synthetic 5,000-camera performance smoke passed. Installed-
account acceptance and deployment remain pending; local harness results are not production
certification. Current and historical evidence are distinguished in `odd/tasks/maps.md`.

## Quick path

1. Open `/maps?mode=live` and choose a site with **Seleccionar sitio** or the operations
   list. This works even if the site has no geographic center or map marker.
2. Check the operations panel: loaded placements and inventory are different datasets.
   Use **Find camera** to select a placed camera and bring its marker into view.
3. If inventory exists but no markers appear, open **Open placement editor** with the
   appropriate permissions. Select a camera in **Sin ubicar**, click its real geographic
   location, and select **Guardar**. No camera is automatically placed by this workflow.
4. Frame the monitoring area and select **Fijar centro de monitoreo aquí**. Reopening that
   site uses its saved center and zoom. Reload to confirm persistence.
5. Return to Live, select a camera, and check actual playback against the real origin.
   A status of online or an HTTP 200 response does not prove working media.

## Modes and permissions

| Mode | Actual workflow | Permissions |
| --- | --- | --- |
| All | Site navigation, placement data, saved layers and filters | `maps.view`; server-side scope filtering remains authoritative |
| Live | Camera availability, placed-camera previews, direct Live handoff | `live.view`; full camera list additionally requires `cameras.view` |
| Investigate | Select a camera; open Events or Playback with its camera ID | `events.view` and/or `recordings.view`; no stream starts just by choosing this mode |
| Analytics | Current overview inventory, placement, availability, and active alarm summaries | `maps.view`; unplaced inventory counts require successful authorized inventory and placement reads |
| Edit | Existing placement/type/FOV/drag editor, zones, CSV, monitoring center | Device writes/CSV: `maps.edit_device`; center: `maps.edit`; zones: `maps.create_zone` |

The existing unplaced endpoint requires `maps.edit` and filters camera inventory using
`cameras.view`. For a device editor with `cameras.view` but without `maps.edit`, the UI can
identify unplaced cameras from successful inventory and placement reads instead. API writes
still enforce their own permissions. UI visibility is not a permission grant; site-scoped
denials remain visible. Account/grant repair is an administrator operation, not automatic.

Analytics is a current-state summary, **not** historical geospatial analytics or a heatmap.
Overview and placements refresh every 30 seconds; inventory refreshes every 15 seconds.
Availability and alarm incident panels use authorized placed-camera data and realtime patches.
Overview aggregates are labeled separately; they need not match the loaded placements.

## Recover an apparently empty map

| Symptom | Check and recovery |
| --- | --- |
| World basemap, no tools for a site | Select the site from the global selector; geographic center is not required for navigation. |
| Inventory exists, zero placed cameras | Commission positions in Editor. Do not guess coordinates or assume imported inventory includes positions. |
| Some or all markers disappear | Use **Show all authorized markers** to reset map filters and enable camera/site layers. This does not broaden authorization. |
| Marker is outside the visible area | Select the placed camera in the operations list to center on its saved position. |
| Request failed/403 | Read the named error; use its Retry action after the cause is corrected. An error is not an empty successful inventory. |
| No authorized sites or invalid `site` URL | Select an accessible site or ask an administrator to verify inventory and scope. |
| No Editor or Live action | Check the permissions above. The page never silently grants access. |
| Tiles fail but controls appear | Verify provider configuration, network/CSP and provider availability. For PMTiles, serve an actual archive, not the SPA HTML fallback. |
| Saved movement conflicts | Use existing Rebase after a revision conflict, inspect the updated draft, and save explicitly. |

Search lists show at most 100 matching cameras; refine the search to reach other inventory.
Changing sites discards unsaved placement/zone drafts rather than carrying edits into another
site. Save or cancel before switching. Site/mode choices create browser history entries;
back/forward and changed URL search parameters update the operational state.

## Final acceptance before deployment

Complete implementation and documentation first, then run the sequential commands listed
under **Deferred final runners and proof** in [`odd/tasks/maps.md`](../../odd/tasks/maps.md).
Do not equate authored regression coverage with executed or passing tests.

- [ ] Current follow-up acceptance: web tests/typecheck/lint/build pass locally; synthetic performance remains unresolved (54 ms writer, 57 ms baseline, 74 ms candidate against 50 ms). Earlier successful harness/Go checks are historical, not proof for this candidate.
- [ ] Multi-site and coordinate-less navigation works for the intended deployed account.
- [ ] Live / Investigate / Analytics have the distinct workflows above; Events and Playback retain camera context.
- [ ] Place, save, reload, drag, save, type/FOV edit, zone operations and center persistence work.
- [ ] Hidden-layer/filter recovery, request failure/retry and denied actions are exercised.
- [ ] Media and recording playback work against a real configured Frigate, not only mocks.
- [ ] Target deployment operation and credential/session are explicitly authorized; deploy the complete solution.
- [ ] Repeat browser acceptance against the deployed version and record results.

No remote probe, permission change, deployment, push or PR is implied by this guide. The
reported deployment URL identifies a symptom; it does not establish its version or grants.

## Camera event notices

With `events.view`, Maps shows new indexed camera alerts/detections over their visible placed
markers without selecting a camera. The latest notice replaces only that camera's previous
notice. Each validated notice stays visible for five seconds; a bounded five-second detail
request budget and one delayed retry do not prolong a notice already displayed.

REST event details remain permission-filtered and authoritative. Labels, severity and plates
are actual indexed values. A permitted available snapshot uses the existing snapshot route;
otherwise the event thumbnail is used. Failed images show **Image unavailable**, never live
video or a fabricated vehicle photo. `snapshots.view` does not grant event access.

These are indexed Frigate review events, not every raw object lifecycle. Push timestamps are
event start times, so the UI accepts starts within the preceding two minutes; events indexed
later may be omitted. Deduplication retains at most 512 event IDs per active context. Site,
identity, permission and camera visibility changes cancel stale requests/notices; camera status
changes do not reset the five-second display. Existing ingestion latency is unchanged.
