import { useSyncExternalStore } from "react";

/** Same breakpoint as the bottom navigation (Tailwind `md` is 768px). */
export const PHONE_QUERY = "(max-width: 767px)";

const supported = () => typeof window !== "undefined" && typeof window.matchMedia === "function";

function subscribe(onChange: () => void) {
  if (!supported()) return () => {};
  const media = window.matchMedia(PHONE_QUERY);
  media.addEventListener("change", onChange);
  return () => media.removeEventListener("change", onChange);
}

const snapshot = () => supported() && window.matchMedia(PHONE_QUERY).matches;

/** useIsPhoneLayout is true below the `md` breakpoint and follows resize and rotation. */
export function useIsPhoneLayout(): boolean {
  return useSyncExternalStore(subscribe, snapshot, () => false);
}
