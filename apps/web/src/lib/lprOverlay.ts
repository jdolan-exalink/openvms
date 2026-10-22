import { queryOptions, useQuery } from "@tanstack/react-query";
import { useSyncExternalStore } from "react";
import { api, unwrap } from "@/api/client";
import { CHANNEL_REFETCH_MS, lprChannelItems } from "@/lib/detectionChannels";

/** Most recent reads kept in the on-video list. */
export const OVERLAY_LIMIT = 6;

export function cameraPlatesQuery(cameraId: string) {
  return queryOptions({
    // Under ["plates"] so the realtime event.created invalidation refreshes it at once.
    queryKey: ["plates", "overlay", cameraId, OVERLAY_LIMIT],
    queryFn: async () =>
      lprChannelItems(unwrap(await api.GET("/api/v1/lpr/reads", { params: { query: { camera_id: [cameraId], limit: OVERLAY_LIMIT } } })).items),
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
