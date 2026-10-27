# Deleting and restoring a camera

Deleting a camera removes it from OpenVMS and from its Frigate server's `config.yml`, without deleting
its history. A deleted camera can be restored with the history it already has.

## Delete

`DELETE /api/v1/cameras/{cameraId}`, or "Eliminar cámara" in the camera's "Más acciones" menu. Needs
`servers.manage` on the camera's server. The caller does not need `servers.config` or
`servers.config.secrets` and never receives any config content: OpenVMS reads and writes the file itself.

What happens, in this order:

1. The camera's block is taken out of Frigate's `config.yml` and Frigate restarts. Every camera on that
   server stops recording for a moment, and the deleted camera stops recording for good. The config
   revision made by this save is kept: it holds the camera's block, which restore puts back. If Frigate
   cannot be reached or rejects the change, nothing changes in OpenVMS (502 or 400).
2. The camera is soft-deleted in OpenVMS (`cameras.deleted_at`), its cells in saved views are emptied,
   and a tombstone is written (`camera_tombstones`) with the recording window the camera had (the longest
   `retain` / `continuous` / `motion` / `alerts` / `detections` days of its `record` section, or the
   global one).
3. The audit log records `CAMERA_REMOVED`.

A camera that is already absent from Frigate's config is still deleted, so a retry after a failure in step
2 finishes the job and recovers what the first attempt saved. The physical device is never touched.

The tombstone stops the Frigate sync from adding the same name again as a new camera; adding a camera with
that name through OpenVMS is refused and points to restore.

## What is kept, and for how long

OpenVMS does not delete the camera's events, plate reads, alarms, tracks or stored thumbnails when it is
deleted. Once the camera's recording window has passed since the deletion, the worker purges them
(`CAMERA_HISTORY_PURGED` in the audit log): events, plate reads, alarms, object tracks, watermarked clips,
notifications, rule firings and the stored objects they point to. A camera whose window was unknown is
never purged. Exports and export jobs, face sightings and object snapshots are kept; the camera row and its
tombstone stay.

Whether Frigate keeps a removed camera's recordings until its own retention expires has not been verified
against a real Frigate (0.17 or 0.18). Do not promise it to anyone until it has.

## Restore

`POST /api/v1/cameras/{cameraId}/restore`, or "Restaurar" under "Cámaras eliminadas" when the cameras list
is filtered by server. Deleted cameras are listed by `GET /api/v1/servers/{serverId}/deleted-cameras`.
Needs `servers.manage`.

The camera comes back as the same row, so its old history joins the recordings that come next and nothing
is overwritten. Its block is put back into Frigate's `config.yml` (Frigate restarts) and the tombstone is
lifted. It is refused with 409 when a live camera already has the same name on that server, in which case
Frigate is not touched, and with 400 when the kept block is no longer stored. If the history was already
purged, the camera is restored without it. Cells removed from saved views are not refilled, and a manual
group keeps the camera.
