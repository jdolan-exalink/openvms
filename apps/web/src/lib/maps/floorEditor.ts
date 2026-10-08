import { queryOptions } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import type { CameraEntity } from "./types";
import { mapWireEntity } from "./api";
export type PlanView = {
  scale: number;
  x: number;
  y: number;
};
export type Point = {
  x: number;
  y: number;
};
export type FloorEntry = Point & {
  revision?: number;
};
type Entries = Record<string, FloorEntry>;
export type FloorDraft = {
  entries: Entries;
  past: Entries[];
  future: Entries[];
};
export const emptyFloorDraft = (): FloorDraft => ({ entries: {}, past: [], future: [] });
export function stageFloor(draft: FloorDraft, id: string, point: Point, revision?: number): FloorDraft {
  return { entries: { ...draft.entries, [id]: { ...point, revision: draft.entries[id]?.revision ?? revision } }, past: [...draft.past, draft.entries], future: [] };
}
export function unstageFloor(draft: FloorDraft, id: string): FloorDraft {
  const { [id]: _, ...entries } = draft.entries;
  return { entries, past: [...draft.past, draft.entries], future: [] };
}
export function undoFloor(draft: FloorDraft): FloorDraft {
  const entries = draft.past.at(-1);
  return entries ? { entries, past: draft.past.slice(0, -1), future: [draft.entries, ...draft.future] } : draft;
}
export function redoFloor(draft: FloorDraft): FloorDraft {
  const entries = draft.future[0];
  return entries ? { entries, past: [...draft.past, draft.entries], future: draft.future.slice(1) } : draft;
}
/** Canvas rect is the fitted, untransformed image size. Pan/zoom affect only projection. */
export function floorPoint(rect: {
  left: number;
  top: number;
  width: number;
  height: number;
}, view: PlanView, clientX: number, clientY: number): Point | undefined {
  const x = (clientX - rect.left - view.x) / (rect.width * view.scale);
  const y = (clientY - rect.top - view.y) / (rect.height * view.scale);
  return Number.isFinite(x) && Number.isFinite(y) && x >= 0 && y >= 0 && x <= 1 && y <= 1 ? { x, y } : undefined;
}
export const floorEntitiesQuery = (siteId: string, floorId: string) => queryOptions({
  queryKey: ["maps", "sites", siteId, "entities", "floor", floorId], enabled: !!siteId && !!floorId,
  queryFn: async ({ signal }) => {
    const res = unwrap(await api.GET("/api/v1/maps/sites/{siteId}/entities", { params: { path: { siteId }, query: { floor_id: floorId } }, signal }));
    return res.entities.map(mapWireEntity).filter(entity => entity.siteId === siteId && entity.position.kind === "floor" && entity.position.floorId === floorId);
  }, refetchInterval: 30000,
});
export const floorUnplacedQuery = (siteId: string, floorId: string) => queryOptions({
  queryKey: ["maps", "unplaced", siteId, "floor", floorId], enabled: !!siteId && !!floorId,
  queryFn: async ({ signal }) => unwrap(await api.GET("/api/v1/maps/unplaced", { params: { query: { site_id: siteId, floor_id: floorId } }, signal })).cameras,
});
export async function saveFloorPlacement(siteId: string, floorId: string, cameraId: string, entry: FloorEntry, camera?: CameraEntity["camera"]) {
  if (!Number.isFinite(entry.x) || !Number.isFinite(entry.y) || entry.x < 0 || entry.y < 0 || entry.x > 1 || entry.y > 1)
    throw new Error("Invalid floor coordinates.");
  return unwrap(await api.PUT("/api/v1/maps/placements/{entityType}/{entityId}", {
    params: { path: { entityType: "camera", entityId: cameraId }, ...(entry.revision !== undefined ? { header: { "If-Match": `"${entry.revision}"` } } : {}) },
    body: { site_id: siteId, floor_id: floorId, x: entry.x, y: entry.y, ...(camera ? { bearing_deg: camera.bearingDeg ?? undefined, fov_deg: camera.fovDeg, range_m: camera.rangeM, props: { camera_type: camera.cameraType, ptz: camera.ptz, lpr: camera.lpr } as never } : {}) },
  }));
}

export async function unplaceFloorCamera(cameraId: string, revision?: number) {
  return unwrap(await api.DELETE("/api/v1/maps/placements/{placementId}", {
    params: {
      path: { placementId: cameraId },
      ...(revision !== undefined ? { header: { "If-Match": `"${revision}"` } } : {}),
    },
  }));
}
