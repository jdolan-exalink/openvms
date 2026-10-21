import { queryOptions, useQuery } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import { plateCropUrl } from "@/components/plates/PlateReadCard";

/**
 * Detection channels are virtual Live tiles that behave like a camera: instead of video they show
 * the latest LPR read or face detection large, with the previous ones underneath. The same view is
 * used by the Live grid, saved views and the fullscreen totem.
 */
export const LPR_CHANNEL_ID = "visionai:lpr-channel";
export const FACES_CHANNEL_ID = "visionai:faces-channel";

export type ChannelKind = "lpr" | "faces";

/** Latest detection plus this many previous ones. */
export const CHANNEL_HISTORY = 8;
/** Polling keeps the channel fresh; realtime invalidation of ["plates"] speeds up LPR further. */
export const CHANNEL_REFETCH_MS = 3000;

export function channelKindOf(id: string): ChannelKind | null {
  if (id === LPR_CHANNEL_ID) return "lpr";
  if (id === FACES_CHANNEL_ID) return "faces";
  return null;
}

export interface ChannelItem {
  key: string;
  kind: ChannelKind;
  /** Plate text, or the recognized person's name ("Desconocido" when unrecognized). */
  label: string;
  recognized?: boolean;
  imageUrl: string;
  cameraName: string;
  zone?: string;
  at: string;
}

const byNewest = (a: ChannelItem, b: ChannelItem) => Date.parse(b.at) - Date.parse(a.at);

export function lprChannelItems(reads: readonly Schemas["PlateRead"][]): ChannelItem[] {
  return reads
    .map((r): ChannelItem => ({
      key: r.id, kind: "lpr", label: r.plate, imageUrl: plateCropUrl(r.id),
      cameraName: r.camera_name, zone: r.zones[0], at: r.seen_at,
    }))
    .sort(byNewest);
}

export function faceChannelItems(events: readonly Schemas["Event"][]): ChannelItem[] {
  return events
    .map((e): ChannelItem => {
      const name = e.sub_labels.find((s) => s.trim() !== "");
      return {
        key: e.id, kind: "faces", label: name ?? "Desconocido", recognized: Boolean(name),
        imageUrl: `/api/v1/events/${encodeURIComponent(e.id)}/thumbnail`,
        cameraName: e.camera_name, zone: e.zones[0], at: e.start_time,
      };
    })
    .sort(byNewest);
}

export function channelQuery(kind: ChannelKind) {
  const limit = CHANNEL_HISTORY + 1;
  return kind === "lpr"
    ? queryOptions({
        // Under ["plates"] so the realtime event.created invalidation refreshes it at once.
        queryKey: ["plates", "channel", limit],
        queryFn: async () => lprChannelItems(unwrap(await api.GET("/api/v1/lpr/reads", { params: { query: { limit } } })).items),
        refetchInterval: CHANNEL_REFETCH_MS,
      })
    : queryOptions({
        queryKey: ["events", "faces-channel", limit],
        queryFn: async () => faceChannelItems(unwrap(await api.GET("/api/v1/events", { params: { query: { label: ["face"], limit } } })).items),
        refetchInterval: CHANNEL_REFETCH_MS,
      });
}

export function useChannelItems(kind: ChannelKind) {
  return useQuery(channelQuery(kind));
}
