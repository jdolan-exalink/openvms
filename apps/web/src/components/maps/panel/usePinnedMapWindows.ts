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
  const key = userId ? pinnedWindowsKey(tenantId ?? null, userId, scope) : "";
  const [stored, setStored] = useState<{ key: string; windows: PinnedWindow[] }>(() => ({
    key,
    windows: key ? loadPinnedWindows(key) : [],
  }));

  // Discard a previous user's/map's windows in the same render, before they can be shown.
  if (stored.key !== key) {
    setStored({ key, windows: key ? loadPinnedWindows(key) : [] });
  }
  const windows = stored.key === key ? stored.windows : [];

  useEffect(() => {
    if (key && stored.key === key) savePinnedWindows(key, stored.windows);
  }, [key, stored]);

  const update = useCallback((change: (windows: PinnedWindow[]) => PinnedWindow[]) => {
    if (!key) return;
    setStored((current) => ({
      key,
      windows: change(current.key === key ? current.windows : loadPinnedWindows(key)),
    }));
  }, [key]);

  const pin = useCallback((id: string) => {
    update((current) => pinWindow(current, id, measureMapStage()));
  }, [update]);
  const unpin = useCallback((id: string) => {
    update((current) => unpinWindow(current, id));
  }, [update]);
  const move = useCallback((id: string, x: number, y: number) => {
    update((current) => moveWindow(current, id, x, y, measureMapStage()));
  }, [update]);
  const arrange = useCallback(() => {
    update((current) => arrangeWindows(current, measureMapStage()));
  }, [update]);

  return { windows, ready: Boolean(key) && stored.key === key, pin, unpin, move, arrange };
}
