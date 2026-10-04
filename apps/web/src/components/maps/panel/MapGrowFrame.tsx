import { createContext, useContext, useEffect, useLayoutEffect, useRef, type ReactNode } from "react";

export type GrowRect = { left: number; top: number; width: number; height: number };

const GROW_MS = 420;
const GrowCloseContext = createContext<() => void>(() => {});

export function useGrowClose() {
  return useContext(GrowCloseContext);
}

function reducedMotion() {
  return typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/** Uniform scale that places the large card on top of the small source. */
function transformFrom(card: HTMLElement, origin: GrowRect) {
  const previous = card.style.transform;
  card.style.transform = "none";
  const dest = card.getBoundingClientRect();
  card.style.transform = previous;
  if (dest.width < 24 || dest.height < 24 || origin.width < 24 || origin.height < 24) return "";
  const scale = origin.width / dest.width;
  const dx = origin.left + origin.width / 2 - (dest.left + dest.width / 2);
  const dy = origin.top + origin.height / 2 - (dest.top + dest.height / 2);
  return `translate(${dx}px, ${dy}px) scale(${scale})`;
}

/**
 * Centers a floating card and grows it from a measured source. Closing plays the
 * same motion backwards before unmounting.
 */
export function MapGrowFrame({
  label,
  origin,
  onClose,
  closeOnEscape = true,
  fixed = false,
  scrim = false,
  className = "",
  children,
}: {
  label: string;
  origin?: GrowRect;
  onClose: () => void;
  closeOnEscape?: boolean;
  /** Viewport overlay. The map uses absolute positioning inside its own frame. */
  fixed?: boolean;
  /** Dims the page and closes when that dimmed area is clicked. */
  scrim?: boolean;
  className?: string;
  children: ReactNode;
}) {
  const cardRef = useRef<HTMLDivElement>(null);
  const fromRef = useRef("");
  const closedRef = useRef(false);
  const closingRef = useRef(false);
  const finish = () => {
    if (closedRef.current) return;
    closedRef.current = true;
    onClose();
  };
  const requestClose = () => {
    if (closingRef.current) return;
    closingRef.current = true;
    const card = cardRef.current;
    const from = (card && origin && transformFrom(card, origin)) || card?.dataset.growFrom || fromRef.current;
    if (!card || !from || reducedMotion()) {
      finish();
      return;
    }
    const done = (event: TransitionEvent) => {
      if (event.target === card && event.propertyName === "transform") finish();
    };
    card.addEventListener("transitionend", done);
    window.setTimeout(finish, GROW_MS + 80);
    card.style.transition = `transform ${GROW_MS}ms cubic-bezier(0.16, 1, 0.3, 1)`;
    card.style.transform = from;
  };
  const requestCloseRef = useRef(requestClose);
  requestCloseRef.current = requestClose;
  useLayoutEffect(() => {
    const card = cardRef.current;
    if (!card || reducedMotion()) return;
    const from = (origin && transformFrom(card, origin)) || "translate(0px, 0px) scale(0.22)";
    fromRef.current = from;
    card.dataset.growFrom = from;
    card.style.transform = from;
    const frame = requestAnimationFrame(() => {
      card.style.transition = `transform ${GROW_MS}ms cubic-bezier(0.16, 1, 0.3, 1)`;
      card.style.transform = "translate(0px, 0px) scale(1)";
    });
    const settle = (event: TransitionEvent) => {
      if (event.target !== card || event.propertyName !== "transform" || closingRef.current) return;
      pinCard(card);
    };
    card.addEventListener("transitionend", settle);
    return () => {
      cancelAnimationFrame(frame);
      card.removeEventListener("transitionend", settle);
    };
  }, [origin]);
  useEffect(() => {
    if (!closeOnEscape) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") requestCloseRef.current();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [closeOnEscape]);
  useEffect(() => {
    const card = cardRef.current;
    if (!card) return;
    const onDown = (event: PointerEvent) => {
      const handle = (event.target as HTMLElement | null)?.closest?.("[data-map-drag]");
      if (!handle || !card.contains(handle)) return;
      if ((event.target as HTMLElement | null)?.closest?.("button, a, input, textarea, select")) return;
      event.preventDefault();
      pinCard(card);
      const startX = event.clientX;
      const startY = event.clientY;
      const startLeft = Number.parseFloat(card.style.left) || 0;
      const startTop = Number.parseFloat(card.style.top) || 0;
      const boundsOf = () => document.querySelector("[data-map-stage]")?.getBoundingClientRect() ?? card.parentElement?.getBoundingClientRect();
      const move = (ev: PointerEvent) => {
        let left = startLeft + ev.clientX - startX;
        let top = startTop + ev.clientY - startY;
        card.style.left = `${left}px`;
        card.style.top = `${top}px`;
        const bounds = boundsOf();
        if (bounds) {
          const next = card.getBoundingClientRect();
          if (next.left < bounds.left) left += bounds.left - next.left;
          else if (next.right > bounds.right) left -= next.right - bounds.right;
          if (next.top < bounds.top) top += bounds.top - next.top;
          else if (next.bottom > bounds.bottom) top -= next.bottom - bounds.bottom;
          card.style.left = `${left}px`;
          card.style.top = `${top}px`;
        }
      };
      const up = () => {
        window.removeEventListener("pointermove", move);
        window.removeEventListener("pointerup", up);
      };
      window.addEventListener("pointermove", move);
      window.addEventListener("pointerup", up);
    };
    card.addEventListener("pointerdown", onDown);
    return () => card.removeEventListener("pointerdown", onDown);
  }, []);
  return (
    <div className={`pointer-events-none inset-0 flex items-center justify-center p-4 md:p-10 ${fixed ? "fixed" : "absolute"} ${className}`}>
      {scrim && (
        <button type="button" aria-label="Cerrar" className="pointer-events-auto absolute inset-0 bg-scrim" onClick={() => requestCloseRef.current()} />
      )}
      <GrowCloseContext.Provider value={requestClose}>
        <div
          ref={cardRef}
          role="dialog"
          aria-label={label}
          className="pointer-events-auto relative z-10 w-[min(56rem,100%)] origin-center overflow-hidden rounded-m3-xl bg-video shadow-2xl"
        >
          {children}
        </div>
      </GrowCloseContext.Provider>
    </div>
  );
}

/** Leaves the card where it is, without a transform, so its chrome stacks above the surface video. */
function pinCard(card: HTMLElement) {
  const parent = card.offsetParent instanceof HTMLElement ? card.offsetParent : null;
  if (!parent) return;
  const rect = card.getBoundingClientRect();
  const parentRect = parent.getBoundingClientRect();
  card.style.transition = "none";
  card.style.transform = "none";
  card.style.position = "absolute";
  card.style.margin = "0";
  card.style.left = `${rect.left - parentRect.left}px`;
  card.style.top = `${rect.top - parentRect.top}px`;
}

function usableRect(rect: { left: number; top: number; width: number; height: number }): GrowRect | undefined {
  const inView = rect.top < window.innerHeight && rect.top + rect.height > 0 && rect.left < window.innerWidth && rect.left + rect.width > 0;
  if (!inView || rect.width < 16 || rect.height < 12 || rect.width > 520 || rect.height > 420) return undefined;
  return { left: rect.left, top: rect.top, width: rect.width, height: rect.height };
}

/** A clicked control is a valid source when it is a small card or row, not the whole map. */
export function rectFromElement(node: Element | null): GrowRect | undefined {
  if (!(node instanceof HTMLElement)) return undefined;
  return usableRect(node.getBoundingClientRect());
}

/** Prefer a visible preview card, then any other tagged source for this camera. */
export function captureGrowOrigin(cameraId: string): GrowRect | undefined {
  const nodes = [...document.querySelectorAll<HTMLElement>(`[data-map-source="${CSS.escape(cameraId)}"]`)];
  nodes.sort((a, b) => Number(b.dataset.mapSourceRank ?? 0) - Number(a.dataset.mapSourceRank ?? 0));
  for (const node of nodes) {
    const rect = usableRect(node.getBoundingClientRect());
    if (rect && rect.width >= 24 && rect.height >= 24) return rect;
  }
  return undefined;
}
