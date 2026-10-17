import { useEffect, useRef, useState, useSyncExternalStore } from "react";

/** How often a snapshot on screen is re-fetched. */
export const SNAPSHOT_REFRESH_MS = 10_000;

const subscribeVisibility = (onChange: () => void) => {
  document.addEventListener("visibilitychange", onChange);
  return () => document.removeEventListener("visibilitychange", onChange);
};
const pageVisible = () => document.visibilityState !== "hidden";

/**
 * useVisibleSnapshot keeps a still picture fresh without streaming: attach `ref` to the tile and
 * use `src`. It re-fetches about every 10 s, but only while the tile intersects the viewport and
 * the page is visible, so a long grid never fetches for tiles that are scrolled away.
 * Without IntersectionObserver the tile counts as visible.
 */
export function useVisibleSnapshot<T extends Element>(url: string) {
  const ref = useRef<T>(null);
  const hasObserver = typeof IntersectionObserver !== "undefined";
  const [onScreen, setOnScreen] = useState(!hasObserver);
  const [stamp, setStamp] = useState(0);
  const lastFetch = useRef(0);
  const page = useSyncExternalStore(subscribeVisibility, pageVisible, () => true);

  useEffect(() => {
    const el = ref.current;
    if (!hasObserver || !el) return;
    const observer = new IntersectionObserver((entries) => {
      const visible = entries.some((entry) => entry.isIntersecting);
      setOnScreen(visible);
      // Scrolling back to a stale tile refreshes it at once instead of after another interval.
      if (visible && lastFetch.current > 0 && Date.now() - lastFetch.current >= SNAPSHOT_REFRESH_MS) {
        lastFetch.current = Date.now();
        setStamp(lastFetch.current);
      } else if (visible && lastFetch.current === 0) {
        lastFetch.current = Date.now();
      }
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [hasObserver]);

  const active = onScreen && page;
  useEffect(() => {
    if (!active) return;
    const timer = setInterval(() => {
      lastFetch.current = Date.now();
      setStamp(lastFetch.current);
    }, SNAPSHOT_REFRESH_MS);
    return () => clearInterval(timer);
  }, [active]);

  return { ref, src: stamp === 0 ? url : `${url}&_=${stamp}` };
}
