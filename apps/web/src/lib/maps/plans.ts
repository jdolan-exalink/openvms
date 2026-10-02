import { api, unwrap } from "@/api/client";
import { PLAN_MAX_BYTES } from "./planConversion";

/** Private binary images use the existing authenticated client, not URLs containing tokens. */
export async function loadFloorPlan(siteId: string, floorId: string, signal: AbortSignal): Promise<Blob> {
  const result = await api.GET("/api/v1/maps/sites/{siteId}/floors/{floorId}/plan", {
    params: { path: {siteId,floorId} }, parseAs: "blob", signal,
  });
  const blob = unwrap(result);
  if (blob.type !== "image/png" || blob.size > PLAN_MAX_BYTES) throw new Error("Invalid plan image response.");
  return blob;
}
export async function uploadFloorPlan(siteId: string, floorId: string, revision: number, blob: Blob, signal?: AbortSignal) {
  if (blob.type !== "image/png" || blob.size > PLAN_MAX_BYTES || !Number.isInteger(revision) || revision < 1) throw new Error("A canonical PNG and current map revision are required.");
  return unwrap(await api.PUT("/api/v1/maps/sites/{siteId}/floors/{floorId}/plan", {
    params: {path:{siteId,floorId},header:{"If-Match":String(revision)}},
    headers:{"Content-Type":"image/png"},
    // OpenAPI binary is represented as string; custom serializer transmits Blob unchanged.
    body: blob as unknown as string, bodySerializer: () => blob, signal,
  }));
}
