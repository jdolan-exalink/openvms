import { queryOptions, useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { api, unwrap } from "@/api/client";
import { CHANNEL_REFETCH_MS, lprChannelItems } from "@/lib/detectionChannels";

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
