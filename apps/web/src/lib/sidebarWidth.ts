/**
 * Context sidebar width (LV-14). The default is also the minimum: it is the narrowest width at
 * which the Live explorer chrome (section padding, deepest tree indent, row actions) still leaves
 * room for names, matching the former fixed `w-64`. The maximum is capped by the viewport in CSS.
 */
export const SIDEBAR_MIN_WIDTH = 256;
export const SIDEBAR_DEFAULT_WIDTH = SIDEBAR_MIN_WIDTH;
export const SIDEBAR_MAX_WIDTH = 480;
export const SIDEBAR_MAX_VIEWPORT_RATIO = 0.4;
export const SIDEBAR_KEYBOARD_STEP = 16;
export const SIDEBAR_WIDTH_KEY = "openvms.live.sidebar.width";

/** Largest allowed width for a viewport: 40% of it, at most 480 px, never below the minimum. */
export function maxSidebarWidth(viewportWidth: number): number {
  return Math.max(SIDEBAR_MIN_WIDTH, Math.min(SIDEBAR_MAX_WIDTH, Math.floor(viewportWidth * SIDEBAR_MAX_VIEWPORT_RATIO)));
}

export function clampSidebarWidth(width: number, viewportWidth: number): number {
  if (!Number.isFinite(width)) return SIDEBAR_DEFAULT_WIDTH;
  return Math.round(Math.min(maxSidebarWidth(viewportWidth), Math.max(SIDEBAR_MIN_WIDTH, width)));
}

export function loadSidebarWidth(): number {
  try {
    const raw = Number(localStorage.getItem(SIDEBAR_WIDTH_KEY));
    return raw > 0 ? Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, raw)) : SIDEBAR_DEFAULT_WIDTH;
  } catch {
    return SIDEBAR_DEFAULT_WIDTH;
  }
}

export function saveSidebarWidth(width: number) {
  try {
    localStorage.setItem(SIDEBAR_WIDTH_KEY, String(width));
  } catch {
    // Storage unavailable: the width still applies for this session.
  }
}
