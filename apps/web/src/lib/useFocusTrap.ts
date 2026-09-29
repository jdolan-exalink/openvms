import { type RefObject, useEffect, useRef } from "react";

const FOCUSABLE = 'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';

const focusables = (container: HTMLElement) =>
  Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE)).filter((el) => !el.hasAttribute("disabled"));

/**
 * Focus management for overlays (dialogs, dropdown panels): on mount focus moves to the first
 * enabled control inside `containerRef` (or the container itself), Tab/Shift+Tab wrap inside it,
 * Escape calls `onEscape`, and on unmount focus returns to `returnFocusTo` (default: whatever had
 * focus when the overlay opened) unless the user already moved focus to another control.
 */
export function useFocusTrap(
  containerRef: RefObject<HTMLElement | null>,
  onEscape: () => void,
  returnFocusTo?: RefObject<HTMLElement | null>,
) {
  const onEscapeRef = useRef(onEscape);
  useEffect(() => {
    onEscapeRef.current = onEscape;
  });

  useEffect(() => {
    const container = containerRef.current;
    const returnTarget = returnFocusTo?.current ?? document.activeElement;
    if (container) (focusables(container)[0] ?? container).focus();

    function onKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        onEscapeRef.current();
        return;
      }
      if (e.key !== "Tab" || !container) return;
      const items = focusables(container);
      const first = items[0];
      const last = items[items.length - 1];
      if (!first || !last) {
        e.preventDefault();
        return;
      }
      if (e.shiftKey && (document.activeElement === first || document.activeElement === container)) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      const active = document.activeElement;
      const lost = !active || active === document.body || !!container?.contains(active);
      if (lost && returnTarget instanceof HTMLElement) returnTarget.focus();
    };
  }, [containerRef, returnFocusTo]);
}
