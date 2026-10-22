import { keepPreviousData, queryOptions, useQuery } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import type { paths } from "@/api/schema";

export type VisionAISource = Schemas["VisionAISource"];
export type VisionAILayout = Schemas["VisionAILayout"];
export type VisionAISlot = Schemas["VisionAISlot"];
/** Accepted inactivity timeouts in seconds, from the API contract. */
export type VisionInactivity = NonNullable<NonNullable<paths["/api/v1/vision-ai/slots"]["get"]["parameters"]["query"]>["inactivity"]>;

/** The ranked slots are re-computed server side; polling is what makes them rotate. */
export const VISION_SLOTS_REFETCH_MS = 2000;

const sources: readonly VisionAISource[] = ["general", "personas", "vehiculos", "lpr", "perimetro", "alarmas"];

/** Maps a LiveExplorer virtual source id (`visionai:<source>`) to an API source; unknown -> general. */
export function visionSourceFromId(id: string): VisionAISource {
  const raw = id.startsWith("visionai:") ? id.slice("visionai:".length) : id;
  return sources.find((s) => s === raw) ?? "general";
}

/** `inactivity` is the birdseye-style timeout in seconds; undefined uses the server default. */
export function visionSlotsQuery(source: VisionAISource, layout: VisionAILayout, inactivity?: VisionInactivity, serverIds?: readonly string[]) {
  const servers = serverIds && serverIds.length > 0 ? [...serverIds].sort() : undefined;
  return queryOptions({
    // The selection is part of the key: each one is its own cached (and server-side allocated) grid.
    queryKey: ["vision-ai", "slots", source, layout, inactivity ?? null, servers ?? null],
    queryFn: async () =>
      unwrap(await api.GET("/api/v1/vision-ai/slots", { params: { query: { source, layout, inactivity, server_id: servers } } })),
    refetchInterval: VISION_SLOTS_REFETCH_MS,
    placeholderData: keepPreviousData,
  });
}

/**
 * `serverIds` limits the grid to those servers; undefined (or empty) shows every server. `enabled`
 * lets the caller hold the request until the selection can be resolved to servers.
 */
export function useVisionSlots(source: VisionAISource, layout: VisionAILayout, inactivity?: VisionInactivity, serverIds?: readonly string[], enabled = true) {
  return useQuery({ ...visionSlotsQuery(source, layout, inactivity, serverIds), enabled });
}

/** Whole seconds since `detectedAt` (never negative), or null when the time is missing or invalid. */
export function detectionAgeSeconds(detectedAt: string | undefined, nowMs: number): number | null {
  if (!detectedAt) return null;
  const at = Date.parse(detectedAt);
  if (Number.isNaN(at)) return null;
  return Math.max(0, Math.floor((nowMs - at) / 1000));
}

export function formatDetectionAge(seconds: number): string {
  if (seconds < 60) return `hace ${seconds}s`;
  return `hace ${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, "0")}s`;
}
