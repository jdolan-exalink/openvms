import type { DigitalZoom } from "./digitalZoom";
import { zoomTransform } from "./digitalZoom";
import type { PlayerSession } from "./PlayerSession";

type Entry = {
  session: PlayerSession;
  /** Per-session container that holds the persistent `<video>` for as long as the session lives. */
  wrapper: HTMLDivElement;
  /** Inner box that receives digital zoom. The `<video>` itself never gets a CSS transform. */
  stage: HTMLDivElement;
  slot: HTMLElement | null;
  /** Ancestors of the slot that clip it (scroll containers), resolved when the slot registers. */
  clippers: HTMLElement[];
  /** Nearest rounded box around the slot (the grid tile); the video copies its corners. */
  rounded: HTMLElement | null;
  playing: boolean;
  applied: string;
  unsubscribe: () => void;
};

const SETTLE_MS = 400;

const now = () => (typeof performance !== "undefined" ? performance.now() : Date.now());
const raf = (cb: () => void): number =>
  typeof requestAnimationFrame === "function" ? requestAnimationFrame(cb) : (setTimeout(cb, 16) as unknown as number);
const cancelRaf = (id: number) => (typeof cancelAnimationFrame === "function" ? cancelAnimationFrame(id) : clearTimeout(id));

function clippingAncestors(el: HTMLElement): HTMLElement[] {
  const out: HTMLElement[] = [];
  for (let p = el.parentElement; p && p !== document.body && p !== document.documentElement; p = p.parentElement) {
    const { overflowX, overflowY } = getComputedStyle(p);
    if (/(auto|scroll|hidden|clip)/.test(overflowX + overflowY)) out.push(p);
  }
  return out;
}

const ROUNDED_LOOKUP_DEPTH = 4;

/** cornerRadii returns top-left, top-right, bottom-right, bottom-left radii (circular part only). */
function cornerRadii(el: HTMLElement): string[] {
  const s = getComputedStyle(el);
  const longhands = [s.borderTopLeftRadius, s.borderTopRightRadius, s.borderBottomRightRadius, s.borderBottomLeftRadius];
  if (longhands.some((v) => parseFloat(v) > 0)) return longhands.map((v) => (v || "0px").split(" ")[0] ?? "0px");
  // Some engines only report the shorthand: expand 1–4 values the way CSS does.
  const parts = (s.borderRadius.split("/")[0] ?? "").trim().split(/\s+/).filter(Boolean);
  const [a = "0px", b = a, c = a, d = b] = parts;
  return [a, b, c, d];
}

const isRounded = (el: HTMLElement) => cornerRadii(el).some((v) => parseFloat(v) > 0);

/**
 * roundedAncestor finds the tile whose corners the video must follow. The video lives in the
 * fixed layer, not inside the tile, so the tile's own overflow clipping never reaches it.
 */
function roundedAncestor(el: HTMLElement): HTMLElement | null {
  let p: HTMLElement | null = el;
  for (let depth = 0; p && depth <= ROUNDED_LOOKUP_DEPTH; depth++, p = p.parentElement) {
    if (isRounded(p)) return p;
  }
  return null;
}

/**
 * VideoSurfaceLayerController hosts the persistent `<video>` of live sessions in one fixed
 * overlay layer and positions each one over the placeholder ("slot") a grid cell registered
 * for it. Because the `<video>` lives in the layer and only the wrapper's position changes,
 * drag and drop, layout changes and expand never re-parent it, which is what pauses a video
 * when the DOM moves it. Digital zoom scales an inner stage, never the `<video>` element.
 * Positions are refreshed (GPU-composited `translate3d`) on ResizeObserver,
 * scroll, resize and pointer/key activity, and then for a short settle period so transitions
 * and dnd-kit transforms are followed frame by frame.
 *
 * Duplicate policy: a session has one `<video>`, so it can be shown in one slot at a time.
 * If two slots register for the same session the most recent one wins; the Live screen never
 * mounts a second player for a camera that is already visible (it shows a snapshot instead).
 */
export class VideoSurfaceLayerController {
  private host: HTMLElement | null = null;
  private readonly entries = new Map<PlayerSession, Entry>();
  private observer: ResizeObserver | null = null;
  private frame = 0;
  private settleUntil = 0;
  private running = false;

  /** setHost binds the layer's container element (null on unmount). */
  setHost(host: HTMLElement | null): void {
    this.host = host;
    if (host) for (const e of this.entries.values()) if (e.wrapper.parentElement !== host) host.appendChild(e.wrapper);
  }

  /** start listens for the events that can move a slot; safe to call again after stop(). */
  start(): void {
    if (this.running) return;
    this.running = true;
    if (typeof ResizeObserver !== "undefined") {
      this.observer = new ResizeObserver(() => this.request());
      for (const e of this.entries.values()) if (e.slot) this.observer.observe(e.slot);
    }
    window.addEventListener("scroll", this.request, { capture: true, passive: true });
    window.addEventListener("resize", this.request, { passive: true });
    document.addEventListener("fullscreenchange", this.request);
    window.addEventListener("pointermove", this.request, { passive: true });
    window.addEventListener("keydown", this.request, { passive: true });
    window.addEventListener("transitionend", this.request, { capture: true, passive: true });
    this.request();
  }

  stop(): void {
    this.running = false;
    window.removeEventListener("scroll", this.request, { capture: true });
    window.removeEventListener("resize", this.request);
    document.removeEventListener("fullscreenchange", this.request);
    window.removeEventListener("pointermove", this.request);
    window.removeEventListener("keydown", this.request);
    window.removeEventListener("transitionend", this.request, { capture: true });
    this.observer?.disconnect();
    this.observer = null;
    if (this.frame) cancelRaf(this.frame);
    this.frame = 0;
  }

  /** preserveOwner refuses competing preview slots instead of stealing an existing surface. */
  /** register shows `session`'s video over `slot`; the returned function releases the slot. */
  register(session: PlayerSession, slot: HTMLElement, preserveOwner = false): () => void {
    const existing = this.entries.get(session);
    if (preserveOwner && existing?.slot && existing.slot !== slot) return () => {};
    const entry = this.entry(session);
    if (entry.slot && entry.slot !== slot) this.observer?.unobserve(entry.slot);
    entry.slot = slot;
    entry.clippers = clippingAncestors(slot);
    entry.rounded = roundedAncestor(slot);
    entry.applied = "";
    this.observer?.observe(slot);
    this.layout(entry);
    this.request();
    return () => {
      if (entry.slot !== slot) return;
      this.observer?.unobserve(slot);
      entry.slot = null;
      entry.clippers = [];
      entry.rounded = null;
      this.layout(entry);
    };
  }

  /** request schedules a position refresh (and keeps refreshing while things are moving). */
  request = (): void => {
    this.settleUntil = now() + SETTLE_MS;
    if (!this.frame && this.running) this.frame = raf(this.tick);
  };

  /**
   * setPictureZoom scales the inner stage of a session. The wrapper keeps its slot position.
   * Passing 1× clears the zoom. Unknown sessions are ignored.
   */
  setPictureZoom(session: PlayerSession, zoom: DigitalZoom): void {
    const entry = this.entries.get(session);
    if (!entry) return;
    entry.stage.style.transformOrigin = "0 0";
    entry.stage.style.transform = zoomTransform(zoom);
  }

  /** layoutNow repositions every video synchronously (used by tests and after registration). */
  layoutNow(): boolean {
    let changed = false;
    for (const e of this.entries.values()) changed = this.layout(e) || changed;
    return changed;
  }

  private tick = (): void => {
    this.frame = 0;
    const changed = this.layoutNow();
    if (this.running && (changed || now() < this.settleUntil)) this.frame = raf(this.tick);
  };

  private entry(session: PlayerSession): Entry {
    const existing = this.entries.get(session);
    if (existing) return existing;
    const wrapper = document.createElement("div");
    wrapper.setAttribute("data-video-surface", session.cameraId);
    Object.assign(wrapper.style, {
      position: "absolute",
      left: "0",
      top: "0",
      overflow: "hidden",
      background: "#000", // video letterbox is physically black in every theme
      pointerEvents: "none",
      willChange: "transform",
      visibility: "hidden",
    } satisfies Partial<CSSStyleDeclaration>);
    const stage = document.createElement("div");
    stage.setAttribute("data-video-zoom", session.cameraId);
    Object.assign(stage.style, {
      position: "absolute",
      top: "0",
      right: "0",
      bottom: "0",
      left: "0",
      width: "100%",
      height: "100%",
      transformOrigin: "0 0",
    } satisfies Partial<CSSStyleDeclaration>);
    wrapper.appendChild(stage);
    this.host?.appendChild(wrapper);
    session.attach(stage);
    const entry: Entry = { session, wrapper, stage, slot: null, clippers: [], rounded: null, playing: false, applied: "", unsubscribe: () => {} };
    const sync = () => {
      const { state } = session.getSnapshot();
      if (state === "EVICTED") return this.drop(entry);
      const playing = state === "ACTIVE" || state === "WARM";
      if (playing !== entry.playing) {
        entry.playing = playing;
        entry.applied = "";
        this.layout(entry);
      }
    };
    entry.unsubscribe = session.subscribe(sync);
    entry.playing = ((s) => s === "ACTIVE" || s === "WARM")(session.getSnapshot().state);
    this.entries.set(session, entry);
    return entry;
  }

  /**
   * restore puts the video back on the layer after a card borrowed it (map preview).
   * Returns false when this session was never registered, so the caller can detach it.
   */
  restore(session: PlayerSession): boolean {
    const entry = this.entries.get(session);
    if (!entry) return false;
    session.attach(entry.stage);
    entry.applied = "";
    this.layout(entry);
    this.request();
    return true;
  }

  /** drop forgets an evicted session and removes its container. */
  private drop(entry: Entry): void {
    entry.unsubscribe();
    if (entry.slot) this.observer?.unobserve(entry.slot);
    entry.wrapper.remove();
    this.entries.delete(entry.session);
  }

  /** layout applies the slot's rect to the wrapper; returns whether anything changed. */
  private layout(e: Entry): boolean {
    const hide = () => this.apply(e, "hidden");
    const slot = e.slot;
    if (!slot || !slot.isConnected || !e.playing) return hide();
    const r = slot.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) return hide();
    // Intersect with every scroll container so the overlay never draws over headers or panels.
    let top = r.top;
    let left = r.left;
    let right = r.right;
    let bottom = r.bottom;
    for (const c of e.clippers) {
      const cr = c.getBoundingClientRect();
      top = Math.max(top, cr.top);
      left = Math.max(left, cr.left);
      right = Math.min(right, cr.right);
      bottom = Math.min(bottom, cr.bottom);
    }
    if (right <= left || bottom <= top) return hide();
    const cut = top > r.top || left > r.left || right < r.right || bottom < r.bottom;
    // Read every frame so a tile whose radius animates (selection) stays clean mid-transition.
    const radii = e.rounded?.isConnected ? cornerRadii(e.rounded) : null;
    const round = radii && radii.some((v) => parseFloat(v) > 0) ? ` round ${radii.join(" ")}` : "";
    const clip = cut || round
      ? `inset(${top - r.top}px ${r.right - right}px ${r.bottom - bottom}px ${left - r.left}px${round})`
      : "none";
    return this.apply(e, `${r.left}|${r.top}|${r.width}|${r.height}|${clip}`, r, clip);
  }

  private apply(e: Entry, key: string, r?: DOMRect, clip = "none"): boolean {
    if (e.applied === key) return false;
    e.applied = key;
    const s = e.wrapper.style;
    if (!r) {
      s.visibility = "hidden";
      return true;
    }
    s.width = `${r.width}px`;
    s.height = `${r.height}px`;
    s.transform = `translate3d(${r.left}px, ${r.top}px, 0)`;
    s.clipPath = clip;
    s.visibility = "visible";
    return true;
  }
}
