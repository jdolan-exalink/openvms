import { api, unwrap } from "@/api/client";
import { ZONE_KIND_COLOR, zoneDraftToPolygon, type ZoneDraft } from "./zoneDraft";

/**
 * saveZone writes the draft with the contract's two verbs: a draft that already knows its
 * zone id patches that zone, an unknown one creates it. The client validated the geometry
 * first, so a failure here means the server refused the write — its message is surfaced
 * verbatim so the operator sees why.
 */
export async function saveZone(siteId: string, draft: ZoneDraft): Promise<void> {
  const geometry = zoneDraftToPolygon(draft) as unknown as Record<string, never>;
  const style = { color: draft.color || ZONE_KIND_COLOR[draft.kind] } as unknown as Record<string, never>;
  if (draft.zoneId) {
    unwrap(
      await api.PATCH("/api/v1/maps/zones/{zoneId}", {
        params: { path: { zoneId: draft.zoneId } },
        body: { name: draft.name, kind: draft.kind, geometry, style },
      }),
    );
    return;
  }
  unwrap(
    await api.POST("/api/v1/maps/sites/{siteId}/zones", {
      params: { path: { siteId } },
      body: { name: draft.name, kind: draft.kind, geometry, style },
    }),
  );
}

/** deleteZone soft-deletes a zone; the caller refetches the site's zone list. */
export async function deleteZone(zoneId: string): Promise<void> {
  unwrap(await api.DELETE("/api/v1/maps/zones/{zoneId}", { params: { path: { zoneId } } }));
}
