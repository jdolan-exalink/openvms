/**
 * Preference for the developer telemetry overlay on the Live grid. Stored per browser; every
 * storage access is guarded because storage can be blocked (private windows, site data off).
 */

export const TELEMETRY_OVERLAY_KEY = "openvms.live.telemetryOverlay";
/** The existing player debug switch also turns the overlay on. */
export const LIVE_DEBUG_KEY = "openvms.live.debug";

function read(key: string): string | null {
  try {
    return typeof localStorage === "undefined" ? null : localStorage.getItem(key);
  } catch {
    return null;
  }
}

/** The value of the page's own switch. */
export function readOverlayPreference(): boolean {
  return read(TELEMETRY_OVERLAY_KEY) === "1";
}

export function writeOverlayPreference(enabled: boolean): void {
  try {
    if (enabled) localStorage.setItem(TELEMETRY_OVERLAY_KEY, "1");
    else localStorage.removeItem(TELEMETRY_OVERLAY_KEY);
  } catch {
    // The choice still applies until the page is closed.
  }
}

/** Whether the Live grid should show the overlay: the saved preference or the debug key. */
export function overlayVisibleOnLive(): boolean {
  return readOverlayPreference() || read(LIVE_DEBUG_KEY) === "1";
}
