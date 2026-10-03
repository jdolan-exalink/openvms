import { useCallback, useEffect, useState } from "react";
import {
  arrangeWindows,
  loadPinnedWindows,
  measureMapStage,
  moveWindow,
  pinWindow,
  pinnedWindowsKey,
  savePinnedWindows,
  unpinWindow,
  type PinnedWindow,
} from "@/lib/maps/pinnedWindows";

/** Restores this map's open camera windows and writes them back after every change. */
export function usePinnedMapWindows(tenantId: string | null | undefined, userId: string | undefined, scope: string) {
  const [windows, setWindows] = useState<PinnedWindow[] | null>(null);
  const key = userId ? pinnedWindowsKey(tenantId ?? null, userId, scope) : "";

  useEffect(() => {
    if (!key) return;
    setWindows(loadPinnedWindows(key));
  }, [key]);

  useEffect(() => {
    if (!key || windows === null) return;
    savePinnedWindows(key, windows);
  }, [key, windows]);

  const pin = useCallback((id: string) => {
    setWindows((current) => pinWindow(current ?? [], id, measureMapStage()));
  }, []);
  const unpin = useCallback((id: string) => {
    setWindows((current) => unpinWindow(current ?? [], id));
  }, []);
  const move = useCallback((id: string, x: number, y: number) => {
    setWindows((current) => moveWindow(current ?? [], id, x, y, measureMapStage()));
  }, []);
  const arrange = useCallback(() => {
    setWindows((current) => arrangeWindows(current ?? [], measureMapStage()));
  }, []);

  return { windows: windows ?? [], ready: windows !== null, pin, unpin, move, arrange };
}
