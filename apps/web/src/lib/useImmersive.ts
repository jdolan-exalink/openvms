import { useCallback, useEffect, useRef, useState, useSyncExternalStore, type RefObject } from "react";

type FullscreenElement = HTMLElement & { webkitRequestFullscreen?: () => Promise<void> | void };
type FullscreenDocument = Document & { webkitFullscreenElement?: Element | null; webkitExitFullscreen?: () => Promise<void> | void };

const LANDSCAPE_QUERY = "(orientation: landscape)";
const supported = () => typeof window !== "undefined" && typeof window.matchMedia === "function";

const subscribeLandscape = (onChange: () => void) => {
  if (!supported()) return () => {};
  const media = window.matchMedia(LANDSCAPE_QUERY);
  media.addEventListener("change", onChange);
  return () => media.removeEventListener("change", onChange);
};

/** useIsLandscape follows the device orientation (reactive). */
export function useIsLandscape(): boolean {
  return useSyncExternalStore(subscribeLandscape, () => supported() && window.matchMedia(LANDSCAPE_QUERY).matches, () => false);
}

const fullscreenElement = () => (document as FullscreenDocument).fullscreenElement ?? (document as FullscreenDocument).webkitFullscreenElement ?? null;

function enterNative(el: FullscreenElement): Promise<void> | undefined {
  const request = el.requestFullscreen?.bind(el) ?? el.webkitRequestFullscreen?.bind(el);
  if (!request) return undefined;
  try {
    return Promise.resolve(request());
  } catch (error) {
    return Promise.reject(error);
  }
}

function exitNative() {
  const doc = document as FullscreenDocument;
  if (!fullscreenElement()) return;
  try {
    void Promise.resolve((doc.exitFullscreen ?? doc.webkitExitFullscreen)?.call(doc)).catch(() => {});
  } catch {
    /* already out */
  }
}

/**
 * useImmersive drives an "immersive" container: the native Fullscreen API when the browser has it
 * (incl. the webkit prefix), and a CSS-only mode otherwise (iPhone Safari/PWA) or when the request
 * is refused. `immersive` is the CSS state either way, so the caller always styles it as fixed
 * inset-0. Leaving native fullscreen from the system UI returns to the previous state, and
 * `active` going false (the view closed) always leaves immersive mode.
 */
export function useImmersive(ref: RefObject<HTMLElement | null>, active: boolean) {
  const [immersive, setImmersive] = useState(false);
  const native = useRef(false);

  useEffect(() => {
    const sync = () => {
      if (native.current && !fullscreenElement()) {
        native.current = false;
        setImmersive(false);
      }
    };
    document.addEventListener("fullscreenchange", sync);
    document.addEventListener("webkitfullscreenchange", sync);
    return () => {
      document.removeEventListener("fullscreenchange", sync);
      document.removeEventListener("webkitfullscreenchange", sync);
    };
  }, []);

  const leave = useCallback(() => {
    native.current = false;
    exitNative();
    setImmersive(false);
  }, []);

  // The view closed: drop the state during render (no extra paint) and leave native fullscreen.
  if (!active && immersive) setImmersive(false);
  useEffect(() => {
    if (!active && native.current) {
      native.current = false;
      exitNative();
    }
  }, [active]);
  // Closing the whole phone Live also leaves fullscreen.
  useEffect(() => () => {
    if (native.current) exitNative();
  }, []);

  const toggle = useCallback(() => {
    if (immersive) {
      leave();
      return;
    }
    setImmersive(true);
    const el = ref.current as FullscreenElement | null;
    const entered = el ? enterNative(el) : undefined;
    if (!entered) return;
    native.current = true;
    entered.catch(() => {
      // Refused (no user activation, policy): the CSS immersive mode stays.
      native.current = false;
    });
  }, [immersive, leave, ref]);

  return { immersive, toggle };
}
