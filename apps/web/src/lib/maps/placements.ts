import { ApiError, api, unwrap } from "@/api/client";
import type { PendingPlacement } from "./placementDraft";

export interface SaveOutcome {
  saved: Array<{ entityId: string; revision: number }>;
  /** 409s keep the server's message so the operator sees why the write was refused. */
  conflicts: Array<{ entityId: string; message: string }>;
  failed: Array<{ entityId: string; message: string }>;
}

/**
 * savePlacements writes one PUT per changed placement, sequentially: every write is an
 * independent optimistic lock, so a stale camera must not hide the ones that did save.
 * If-Match travels only when a revision is known — a camera without a placement would be
 * rejected by the server for sending one.
 */
export async function savePlacements(entries: readonly PendingPlacement[]): Promise<SaveOutcome> {
  const outcome: SaveOutcome = { saved: [], conflicts: [], failed: [] };

  for (const entry of entries) {
    try {
      const placement = unwrap(
        await api.PUT("/api/v1/maps/placements/{entityType}/{entityId}", {
          params: {
            path: { entityType: entry.entityType, entityId: entry.entityId },
            ...(entry.revision !== undefined ? { header: { "If-Match": `"${entry.revision}"` } } : {}),
          },
          body: {
            site_id: entry.siteId,
            lat: entry.lat,
            lng: entry.lng,
            bearing_deg: entry.bearingDeg,
            fov_deg: entry.fovDeg,
            range_m: entry.rangeM,
          },
        }),
      );
      outcome.saved.push({ entityId: entry.entityId, revision: placement.revision });
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (error instanceof ApiError && error.status === 409) outcome.conflicts.push({ entityId: entry.entityId, message });
      else outcome.failed.push({ entityId: entry.entityId, message });
    }
  }

  return outcome;
}
