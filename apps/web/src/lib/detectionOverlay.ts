import { queryOptions, useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { api, unwrap } from "@/api/client";
import { meQuery } from "@/api/queries";
import { CHANNEL_REFETCH_MS, faceChannelItems, lprChannelItems } from "@/lib/detectionChannels";
import { can } from "@/lib/perm";

/** Most recent reads fetched for the on-video list: the most rows a tall list can show. */
export const OVERLAY_FETCH_LIMIT = 30;
/** Rows shown while the height of the list cannot be measured. */
export const OVERLAY_FALLBACK_ROWS = 6;
/** Height of one row (thumbnail, plate and age) and the gap between rows, in px. */
export const OVERLAY_ROW_PX = 52;
export const OVERLAY_ROW_GAP_PX = 6;

/** How many whole rows fit in `heightPx`; the default count when it is not a measurement. */
export function rowsThatFit(heightPx: number): number {
  if (!(heightPx > 0)) return OVERLAY_FALLBACK_ROWS;
  const rows = Math.floor((heightPx + OVERLAY_ROW_GAP_PX) / (OVERLAY_ROW_PX + OVERLAY_ROW_GAP_PX));
  return Math.min(OVERLAY_FETCH_LIMIT, Math.max(1, rows));
}

/**
 * Rows that fit the list element it is attached to, following its size as the tile resizes. Pass
 * the returned ref to the list. Without ResizeObserver (or before a measurement) it is the default.
 */
export function useFittingRows(): [(el: HTMLElement | null) => void, number] {
  const [el, setEl] = useState<HTMLElement | null>(null);
  const [rows, setRows] = useState(OVERLAY_FALLBACK_ROWS);
  const ref = useCallback((node: HTMLElement | null) => setEl(node), []);
  useEffect(() => {
    if (!el || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver((entries) => {
      const entry = entries[entries.length - 1];
      if (entry) setRows(rowsThatFit(entry.contentRect.height));
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [el]);
  return [ref, rows];
}

export function cameraPlatesQuery(cameraId: string) {
  return queryOptions({
    // Under ["plates"] so the realtime event.created invalidation refreshes it at once.
    queryKey: ["plates", "overlay", cameraId, OVERLAY_FETCH_LIMIT],
    queryFn: async () =>
      lprChannelItems(unwrap(await api.GET("/api/v1/lpr/reads", { params: { query: { camera_id: [cameraId], limit: OVERLAY_FETCH_LIMIT } } })).items),
    refetchInterval: CHANNEL_REFETCH_MS,
  });
}

export function useCameraPlates(cameraId: string, enabled = true) {
  return useQuery({ ...cameraPlatesQuery(cameraId), enabled });
}

/**
 * Latest face events of every camera, fetched once and split per camera by the tiles: cameras have
 * an `lpr` flag but no face flag, so which ones have faces is only known from the detections.
 */
export const FACE_FETCH_LIMIT = 200;

export function overlayFacesQuery() {
  return queryOptions({
    queryKey: ["events", "overlay-faces", FACE_FETCH_LIMIT],
    queryFn: async () =>
      faceChannelItems(unwrap(await api.GET("/api/v1/events", { params: { query: { label: ["face"], limit: FACE_FETCH_LIMIT } } })).items),
    refetchInterval: CHANNEL_REFETCH_MS,
  });
}

/** The face detections of one camera; nothing is fetched without the events permission. */
export function useCameraFaces(cameraId: string, enabled = true) {
  const me = useQuery(meQuery);
  return useQuery({
    ...overlayFacesQuery(),
    enabled: enabled && can(me.data, "events.view"),
    select: (items) => items.filter((item) => item.cameraId === cameraId),
  });
}

const COLLAPSED_KEY = "openvms.lprOverlay.collapsed";
const listeners = new Set<() => void>();

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(COLLAPSED_KEY) === "1";
  } catch {
    return false;
  }
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function setCollapsed(next: boolean) {
  try {
    localStorage.setItem(COLLAPSED_KEY, next ? "1" : "0");
  } catch {
    // Storage can be blocked; the choice then only lasts until the next render.
  }
  listeners.forEach((l) => l());
}

/** Whether the user collapsed the plate list. Shared by every overlay, persisted in localStorage. */
export function useLprOverlayCollapsed(): [boolean, (next: boolean) => void] {
  const collapsed = useSyncExternalStore(subscribe, readCollapsed, () => false);
  return [collapsed, setCollapsed];
}
