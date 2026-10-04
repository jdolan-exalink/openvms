import { useSyncExternalStore } from "react";

export const COARSE_QUERY = "(pointer: coarse)";
/** Phones have a short side below this many CSS px; tablets and larger get the desktop Live. */
export const PHONE_SHORT_SIDE_PX = 600;

const supported = () => typeof window !== "undefined" && typeof window.matchMedia === "function";

function subscribe(onChange: () => void) {
  if (!supported()) return () => {};
  const media = window.matchMedia(COARSE_QUERY);
  media.addEventListener("change", onChange);
  return () => media.removeEventListener("change", onChange);
}

/** The short side of the physical screen does not flip on rotation; the viewport is the fallback. */
function shortSide(): number {
  const s = window.screen;
  if (s && s.width > 0 && s.height > 0) return Math.min(s.width, s.height);
  return Math.min(window.innerWidth, window.innerHeight);
}

const snapshot = () => supported() && window.matchMedia(COARSE_QUERY).matches && shortSide() < PHONE_SHORT_SIDE_PX;

/**
 * useIsPhoneDevice is true on touch devices whose short screen side is under 600 CSS px.
 * Tablets, touch laptops, desktops and narrow desktop windows get the desktop Live.
 */
export function useIsPhoneDevice(): boolean {
  return useSyncExternalStore(subscribe, snapshot, () => false);
}
