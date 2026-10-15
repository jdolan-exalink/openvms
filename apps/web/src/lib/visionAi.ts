import { keepPreviousData, queryOptions, useQuery } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";

export type VisionAISource = Schemas["VisionAISource"];
export type VisionAILayout = Schemas["VisionAILayout"];
export type VisionAISlot = Schemas["VisionAISlot"];

/** The ranked slots are re-computed server side; polling is what makes them rotate. */
export const VISION_SLOTS_REFETCH_MS = 2000;

const sources: readonly VisionAISource[] = ["general", "personas", "vehiculos", "lpr", "perimetro", "alarmas"];

/** Maps a LiveExplorer virtual source id (`visionai:<source>`) to an API source; unknown -> general. */
export function visionSourceFromId(id: string): VisionAISource {
  const raw = id.startsWith("visionai:") ? id.slice("visionai:".length) : id;
  return sources.find((s) => s === raw) ?? "general";
}

export function visionSlotsQuery(source: VisionAISource, layout: VisionAILayout) {
  return queryOptions({
    queryKey: ["vision-ai", "slots", source, layout],
    queryFn: async () =>
      unwrap(await api.GET("/api/v1/vision-ai/slots", { params: { query: { source, layout } } })),
    refetchInterval: VISION_SLOTS_REFETCH_MS,
    placeholderData: keepPreviousData,
  });
}

export function useVisionSlots(source: VisionAISource, layout: VisionAILayout) {
  return useQuery(visionSlotsQuery(source, layout));
}
