/** Floating map camera windows, remembered in this browser per user and map. */

export const PINNED_WINDOW_LIMIT = 4;
export const PINNED_WINDOW_WIDTH = 288;
export const PINNED_WINDOW_HEIGHT = 188;

export type PinnedWindow = { id: string; x: number; y: number };
export type StageSize = { width: number; height: number };

export function pinnedWindowsKey(tenantId: string | null, userId: string, scope: string): string {
  return `openvms.maps.windows.v1:${tenantId ?? "platform"}:${userId}:${scope}`;
}

export function parsePinnedWindows(raw: string | null): PinnedWindow[] {
  if (!raw) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  const windows: PinnedWindow[] = [];
  for (const item of parsed) {
    if (!item || typeof item !== "object") continue;
    const id = "id" in item && typeof item.id === "string" ? item.id : "";
    const x = "x" in item && typeof item.x === "number" ? item.x : Number.NaN;
    const y = "y" in item && typeof item.y === "number" ? item.y : Number.NaN;
    if (!id || !Number.isFinite(x) || !Number.isFinite(y) || windows.some((window) => window.id === id)) continue;
    windows.push({ id, x, y });
    if (windows.length >= PINNED_WINDOW_LIMIT) break;
  }
  return windows;
}

export function serializePinnedWindows(windows: PinnedWindow[]): string {
  return JSON.stringify(windows.slice(0, PINNED_WINDOW_LIMIT));
}

export function clampPinnedOrigin(x: number, y: number, stage: StageSize): { x: number; y: number } {
  const maxX = Math.max(0, stage.width - 48);
  const maxY = Math.max(0, stage.height - 32);
  return {
    x: Math.min(Math.max(0, x), maxX),
    y: Math.min(Math.max(0, y), maxY),
  };
}

export function defaultPinnedOrigin(index: number, stage: StageSize): { x: number; y: number } {
  const x = Math.max(8, stage.width - PINNED_WINDOW_WIDTH - 16);
  const stacked = 64 + index * (PINNED_WINDOW_HEIGHT + 8);
  const y = stacked + PINNED_WINDOW_HEIGHT > stage.height && stage.height > PINNED_WINDOW_HEIGHT
    ? Math.max(8, stage.height - PINNED_WINDOW_HEIGHT - 8)
    : stacked;
  return clampPinnedOrigin(x, y, stage);
}

/** Adds a window, keeping the newest four. An id already open stays where it is. */
export function pinWindow(windows: PinnedWindow[], id: string, stage: StageSize): PinnedWindow[] {
  if (windows.some((window) => window.id === id)) return windows;
  const kept = windows.length >= PINNED_WINDOW_LIMIT ? windows.slice(1) : windows;
  return [...kept, { id, ...defaultPinnedOrigin(kept.length, stage) }];
}

export function unpinWindow(windows: PinnedWindow[], id: string): PinnedWindow[] {
  return windows.filter((window) => window.id !== id);
}

export function moveWindow(windows: PinnedWindow[], id: string, x: number, y: number, stage: StageSize): PinnedWindow[] {
  const next = clampPinnedOrigin(x, y, stage);
  return windows.map((window) => (window.id === id ? { ...window, ...next } : window));
}

/** Packs open windows down the right side, under the map bar. */
export function arrangeWindows(windows: PinnedWindow[], stage: StageSize): PinnedWindow[] {
  return windows.map((window, index) => ({ id: window.id, ...defaultPinnedOrigin(index, stage) }));
}

export function loadPinnedWindows(key: string): PinnedWindow[] {
  if (typeof window === "undefined") return [];
  try {
    return parsePinnedWindows(window.localStorage.getItem(key));
  } catch {
    return [];
  }
}

export function savePinnedWindows(key: string, windows: PinnedWindow[]): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(key, serializePinnedWindows(windows));
  } catch {
    // Private mode and denied storage leave the session memory in place.
  }
}

export function measureMapStage(): StageSize {
  const rect = document.querySelector("[data-map-stage]")?.getBoundingClientRect();
  return {
    width: rect && rect.width > 0 ? rect.width : 1280,
    height: rect && rect.height > 0 ? rect.height : 720,
  };
}
