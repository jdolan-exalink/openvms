import { queryOptions, useQuery } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import type { VisionAISlot } from "@/lib/visionAi";

export type EventsTab = "active" | "history" | "lpr" | "faces";

/** How many rows the historic, LPR and face tabs load. */
export const TAB_LIMIT = 30;
const TAB_REFETCH_MS = 5000;

/**
 * One shape for everything the Vision AI side bar lists and the popup shows, whatever it came from:
 * an active slot, a historic event, a plate read or a face event.
 */
export type VisionEventView = {
  key: string;
  source: "slot" | "event" | "plate";
  cameraName: string;
  object?: string;
  /** 0..1 */
  confidence?: number;
  zone?: string;
  plate?: string;
  /** Recognized person of a face event. */
  person?: string;
  /** ISO time of the detection. */
  at?: string;
  /** Image URLs in preference order; the viewer falls through them on load errors. */
  images: string[];
  /** Set for active slots, so the popup can offer to pin them in the focus view. */
  slot?: VisionAISlot;
};

const eventImages = (id: string, snapshot: boolean, thumbnail: boolean, canSnapshots: boolean): string[] => [
  ...(snapshot && canSnapshots ? [`/media/v1/events/${id}/snapshot.jpg`] : []),
  ...(thumbnail ? [`/api/v1/events/${id}/thumbnail`] : []),
];

export function slotToView(slot: VisionAISlot, canSnapshots: boolean): VisionEventView {
  return {
    key: `slot:${slot.slot}:${slot.event_id ?? slot.camera_id ?? ""}`,
    source: "slot",
    cameraName: slot.camera_name ?? "Cámara",
    object: slot.object,
    confidence: slot.confidence,
    zone: slot.zone,
    plate: slot.plate,
    at: slot.assigned_at,
    images: slot.event_id ? eventImages(slot.event_id, true, true, canSnapshots) : [],
    slot,
  };
}

export function eventToView(event: Schemas["Event"], canSnapshots: boolean): VisionEventView {
  const person = event.labels.includes("face") ? event.sub_labels.find((s) => s.trim() !== "") : undefined;
  return {
    key: `event:${event.id}`,
    source: "event",
    cameraName: event.camera_name,
    object: event.labels[0],
    zone: event.zones[0],
    plate: event.plates[0],
    person,
    at: event.start_time,
    images: eventImages(event.id, event.has_snapshot, event.has_thumbnail, canSnapshots),
  };
}

export function plateToView(read: Schemas["PlateRead"], canSnapshots: boolean): VisionEventView {
  const base = `/media/v1/lpr/reads/${encodeURIComponent(read.id)}/snapshot.jpg`;
  return {
    key: `plate:${read.id}`,
    source: "plate",
    cameraName: read.camera_name,
    object: read.label,
    confidence: read.score ?? undefined,
    zone: read.zones[0],
    plate: read.plate,
    at: read.seen_at,
    images: [...(canSnapshots ? [base] : []), `${base}?crop=1&quality=55`],
  };
}

/**
 * Which servers a list is limited to. `serverIds` filters the events lists; plate reads can only be
 * filtered by camera, so the caller sends the cameras of those servers. Undefined means every server.
 */
export type ListScope = { serverIds?: readonly string[]; cameraIds?: readonly string[] };

const scopeKey = (ids?: readonly string[]) => (ids && ids.length > 0 ? [...ids].sort() : null);
const nonEmpty = <T,>(ids?: readonly T[]) => (ids && ids.length > 0 ? [...ids] : undefined);

const historyQuery = (scope: ListScope) =>
  queryOptions({
    queryKey: ["events", "vision-panel", "history", TAB_LIMIT, scopeKey(scope.serverIds)],
    queryFn: async () => unwrap(await api.GET("/api/v1/events", { params: { query: { limit: TAB_LIMIT, server_id: nonEmpty(scope.serverIds) } } })).items,
    refetchInterval: TAB_REFETCH_MS,
  });

const facesQuery = (scope: ListScope) =>
  queryOptions({
    queryKey: ["events", "vision-panel", "faces", TAB_LIMIT, scopeKey(scope.serverIds)],
    queryFn: async () =>
      unwrap(await api.GET("/api/v1/events", { params: { query: { label: ["face"], limit: TAB_LIMIT, server_id: nonEmpty(scope.serverIds) } } })).items,
    refetchInterval: TAB_REFETCH_MS,
  });

const platesQuery = (scope: ListScope) =>
  queryOptions({
    // Under ["plates"] so the realtime event.created invalidation refreshes it at once.
    queryKey: ["plates", "vision-panel", TAB_LIMIT, scopeKey(scope.cameraIds)],
    queryFn: async () =>
      unwrap(await api.GET("/api/v1/lpr/reads", { params: { query: { limit: TAB_LIMIT, camera_id: nonEmpty(scope.cameraIds) } } })).items,
    refetchInterval: TAB_REFETCH_MS,
  });

/** How many plate reads the LPR mode lists. */
export const LPR_MODE_LIMIT = 100;

const lprModeQuery = (scope: ListScope) =>
  queryOptions({
    // Under ["plates"] so the realtime event.created invalidation refreshes it at once.
    queryKey: ["plates", "vision-lpr", LPR_MODE_LIMIT, scopeKey(scope.cameraIds)],
    queryFn: async () =>
      unwrap(await api.GET("/api/v1/lpr/reads", { params: { query: { limit: LPR_MODE_LIMIT, camera_id: nonEmpty(scope.cameraIds) } } })).items,
    refetchInterval: TAB_REFETCH_MS,
  });

/** The plate reads the LPR mode lists, newest first, as event views. Nothing is fetched while disabled. */
export function usePlateReads(canSnapshots: boolean, enabled: boolean, scope: ListScope = {}) {
  const q = useQuery({ ...lprModeQuery(scope), enabled });
  return { views: (q.data ?? []).map((r) => plateToView(r, canSnapshots)), loading: enabled && q.isPending, error: q.isError };
}

/** Rows of a tab other than "active"; nothing is fetched until the tab is open. */
export function useTabEvents(tab: EventsTab, canSnapshots: boolean, scope: ListScope = {}) {
  const history = useQuery({ ...historyQuery(scope), enabled: tab === "history" });
  const faces = useQuery({ ...facesQuery(scope), enabled: tab === "faces" });
  const plates = useQuery({ ...platesQuery(scope), enabled: tab === "lpr" });
  if (tab === "history") return { views: (history.data ?? []).map((e) => eventToView(e, canSnapshots)), loading: history.isPending, error: history.isError };
  if (tab === "faces") return { views: (faces.data ?? []).map((e) => eventToView(e, canSnapshots)), loading: faces.isPending, error: faces.isError };
  if (tab === "lpr") return { views: (plates.data ?? []).map((r) => plateToView(r, canSnapshots)), loading: plates.isPending, error: plates.isError };
  return { views: [] as VisionEventView[], loading: false, error: false };
}
