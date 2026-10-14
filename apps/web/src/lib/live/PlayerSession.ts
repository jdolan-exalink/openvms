import { reconnectDelay } from "./backoff";
import { PlayerStateMachine, type PlayerState, type StateTransition } from "./playerState";
import { observeFirstFrame, playerMetrics, type PlayerMetrics } from "./playerMetrics";
import type { ServerBackoff } from "./serverBackoff";
import { describeStreamError, streamError, type SessionError } from "./streamErrors";

// Codecs offered to go2rtc, most preferred first (same list go2rtc's own player uses).
const CODECS = [
  "avc1.640029",
  "avc1.64002A",
  "avc1.640033",
  "hvc1.1.6.L153.B0",
  "mp4a.40.2",
  "mp4a.40.5",
  "flac",
  "opus",
];

type MediaSourceCtor = typeof MediaSource;

function mediaSourceCtor(): MediaSourceCtor | undefined {
  return window.MediaSource ?? (window as unknown as { ManagedMediaSource?: MediaSourceCtor }).ManagedMediaSource;
}

function supportedCodecs(MS: MediaSourceCtor): string {
  return CODECS.filter((c) => MS.isTypeSupported(`video/mp4; codecs="${c}"`)).join();
}

export type SessionQuality = "sub" | "main";

/**
 * What a view needs to render a session: the lifecycle state, a user-facing message, the
 * current stream error (resilient sessions only) and an object URL of the last captured frame.
 */
export type SessionSnapshot = {
  readonly state: PlayerState;
  readonly message: string;
  readonly error: SessionError | null;
  readonly poster: string | null;
};

const INITIAL_SNAPSHOT: SessionSnapshot = { state: "UNINITIALIZED", message: "", error: null, poster: null };

/** How often an active resilient session refreshes its in-memory last frame. */
const FRAME_CAPTURE_INTERVAL_MS = 20_000;
const FRAME_MAX_WIDTH = 640;

export type PlayerSessionOptions = {
  cameraId: string;
  quality: SessionQuality;
  muted?: boolean;
  /** Called with the gateway's error text when go2rtc reports a stream error. */
  onError?: (message: string) => void;
  /** Injectable for tests; defaults to `new WebSocket(url)`. */
  createSocket?: (url: string) => WebSocket;
  /** Jitter source in [0, 1); injectable for tests. */
  random?: () => number;
  metrics?: PlayerMetrics;
  now?: () => number;
  /** Frigate server the camera belongs to; sessions of one server back off together. */
  serverId?: string;
  /**
   * Opt in to the Live View platform behaviour: gateway error codes with stop/retry policy,
   * last-frame capture, group backoff, suspension while the tab is hidden and resuming the
   * `<video>` after DOM moves. Off keeps the legacy player behaviour.
   */
  resilient?: boolean;
  backoff?: ServerBackoff;
};

/**
 * PlayerSession owns everything needed to show one camera live: a single `<video>` element
 * (created imperatively, never by React), the MSE websocket and MediaSource/SourceBuffer
 * pipeline, the reconnect policy and the lifecycle state machine. The video can be moved
 * between DOM containers with attach()/detach() without touching the pipeline, so moving a
 * tile on screen never reconnects the stream.
 */
export class PlayerSession {
  readonly cameraId: string;
  readonly quality: SessionQuality;
  readonly video: HTMLVideoElement;

  private readonly machine: PlayerStateMachine;
  private readonly metrics: PlayerMetrics;
  private readonly createSocket: (url: string) => WebSocket;
  private readonly random: () => number;
  private readonly errorListeners = new Set<(message: string) => void>();
  private readonly listeners = new Set<() => void>();
  private snapshot: SessionSnapshot = INITIAL_SNAPSHOT;
  private readonly serverId?: string;
  private readonly resilient: boolean;
  private readonly backoff?: ServerBackoff;
  private readonly suspendReasons = new Set<string>();
  private cancelRetry: (() => void) | undefined;
  private frameTimer: ReturnType<typeof setInterval> | undefined;
  private canvas: HTMLCanvasElement | null = null;
  /** True after a permanent error: no automatic retry until retryNow(). */
  private stopped = false;

  private ws: WebSocket | null = null;
  private closed = false;
  private started = false;
  private warm = false;
  private retry = 0;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private objectURL = "";
  private cancelFrame: (() => void) | undefined;
  private connectCount = 0;
  private boxObserver: ResizeObserver | undefined;

  constructor(opts: PlayerSessionOptions) {
    this.cameraId = opts.cameraId;
    this.quality = opts.quality;
    this.metrics = opts.metrics ?? playerMetrics;
    this.createSocket = opts.createSocket ?? ((url) => new WebSocket(url));
    this.random = opts.random ?? Math.random;
    this.serverId = opts.serverId;
    this.resilient = opts.resilient ?? false;
    this.backoff = opts.backoff;
    if (opts.onError) this.errorListeners.add(opts.onError);
    this.machine = new PlayerStateMachine({
      cameraId: opts.cameraId,
      quality: opts.quality,
      now: opts.now,
      metrics: this.metrics,
    });
    this.video = document.createElement("video");
    this.video.className = "size-full object-fill";
    this.video.autoplay = true;
    this.video.playsInline = true;
    // A <video> paints opaque black before the first frame. Stay invisible until
    // the decoder has a picture so the snapshot underneath remains visible.
    this.video.style.opacity = "0";
    this.video.addEventListener("loadeddata", this.revealFrame);
    this.video.addEventListener("resize", this.revealFrame);
    this.video.addEventListener("playing", this.revealFrame);
    this.setMuted(opts.muted ?? true);
  }

  /** revealFrame shows the element once the decoder has a real picture. */
  private revealFrame = (): void => {
    if (this.video.videoWidth > 0) this.video.style.opacity = "1";
  };

  // ---- observable state -------------------------------------------------------------

  get state(): PlayerState {
    return this.machine.state;
  }

  get history(): readonly StateTransition[] {
    return this.machine.history;
  }

  /** Number of websocket connections this session has opened (1 means never reconnected). */
  get connections(): number {
    return this.connectCount;
  }

  /** useSyncExternalStore-compatible: the returned object only changes when the snapshot does. */
  /** onServerError subscribes to stream errors reported by the gateway; returns an unsubscribe. */
  onServerError = (listener: (message: string) => void): (() => void) => {
    this.errorListeners.add(listener);
    return () => this.errorListeners.delete(listener);
  };

  getSnapshot = (): SessionSnapshot => this.snapshot;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  // ---- lifecycle --------------------------------------------------------------------

  /** connect starts the pipeline once; calling it again is a no-op. */
  connect(): void {
    if (this.started || this.closed) return;
    this.started = true;
    document.addEventListener("visibilitychange", this.onVisibility);
    if (this.resilient) {
      this.video.addEventListener("pause", this.onPause);
      this.frameTimer = setInterval(() => {
        if (this.state === "ACTIVE" && !document.hidden) this.captureFrame();
      }, FRAME_CAPTURE_INTERVAL_MS);
      if (document.visibilityState === "hidden") this.suspendReasons.add("page-hidden");
    }
    if (this.suspendReasons.size > 0) return; // opens on resume
    this.open();
  }

  /** attach places the persistent `<video>` in `container`, keeping the pipeline running. */
  attach(container: HTMLElement): void {
    if (this.closed) return;
    if (this.video.parentElement !== container) container.appendChild(this.video);
    this.watchBox(container);
    this.resumePlayback();
  }

  /** detach removes the `<video>` from the DOM without closing anything. */
  detach(): void {
    this.boxObserver?.disconnect();
    this.boxObserver = undefined;
    this.video.remove();
  }

  /**
   * watchBox keeps the element filling its box and resumes playback once the box has a
   * real size. A CSS transform on the `<video>` itself makes Chrome paint the frame
   * several times, shifted, so this only sets geometry.
   */
  private watchBox(container: HTMLElement): void {
    this.boxObserver?.disconnect();
    const fit = () => {
      const v = this.video;
      v.style.position = "absolute";
      v.style.inset = "0";
      v.style.width = "100%";
      v.style.height = "100%";
      v.style.transform = "";
      this.revealFrame();
      if (container.clientWidth < 2 || container.clientHeight < 2) return;
      if (v.paused && this.objectURL) void v.play()?.catch(() => {});
    };
    if (typeof ResizeObserver !== "undefined") {
      this.boxObserver = new ResizeObserver(fit);
      this.boxObserver.observe(container);
    }
    fit();
  }

  setMuted(muted: boolean): void {
    this.video.muted = muted;
    if (muted) this.video.setAttribute("muted", "");
    else this.video.removeAttribute("muted");
  }

  /** setObjectFit chooses how the picture fills its box. Undefined keeps the default fill. */
  setObjectFit(fit?: "contain" | "cover" | "fill"): void {
    this.video.style.objectFit = fit ?? "";
  }

  /** markWarm flags the session as running without a visible view (kept for a quick return). */
  markWarm(cause = "released"): void {
    if (this.closed) return;
    this.warm = true;
    // Only a session that is delivering media becomes WARM; the others reach it with their first media.
    if (this.state === "ACTIVE") this.move("WARM", cause);
  }

  /** markActive flags the session as being watched again. */
  markActive(cause = "acquired"): void {
    if (this.closed) return;
    this.warm = false;
    if (this.state === "WARM") this.move("ACTIVE", cause);
  }

  /**
   * setSuspended pauses (on) or resumes (off) the transport for `reason` (e.g. "offscreen",
   * "page-hidden"). While any reason is set the socket is closed and the last frame stays
   * available as the poster; resuming reconnects.
   */
  setSuspended(reason: string, on: boolean): void {
    if (this.closed) return;
    const before = this.suspendReasons.size > 0;
    if (on) this.suspendReasons.add(reason);
    else this.suspendReasons.delete(reason);
    const after = this.suspendReasons.size > 0;
    if (before === after || !this.started) return;
    if (after) {
      if (this.stopped) return;
      this.captureFrame();
      this.cancelPendingOpen();
      this.dropSocket();
      this.move("SUSPENDED", reason);
    } else if (!this.stopped) {
      this.open();
    }
  }

  /** retryNow clears a stopped/backing-off state and reconnects immediately ("Reintentar"). */
  retryNow(): void {
    if (this.closed || !this.started) return;
    this.stopped = false;
    this.retry = 0;
    this.cancelPendingOpen();
    this.move(this.state, "retry", "", null);
    if (this.suspendReasons.size === 0) this.open();
  }

  /** close releases the socket, the media pipeline and the `<video>`; the session is unusable afterwards. */
  close(cause = "closed"): void {
    if (this.closed) return;
    this.closed = true;
    this.cancelPendingOpen();
    clearInterval(this.frameTimer);
    this.cancelFrame?.();
    document.removeEventListener("visibilitychange", this.onVisibility);
    this.video.removeEventListener("pause", this.onPause);
    this.video.removeEventListener("loadeddata", this.revealFrame);
    this.video.removeEventListener("resize", this.revealFrame);
    this.video.removeEventListener("playing", this.revealFrame);
    this.boxObserver?.disconnect();
    this.boxObserver = undefined;
    this.dropSocket();
    if (this.snapshot.poster) URL.revokeObjectURL(this.snapshot.poster);
    this.video.removeAttribute("src");
    this.video.load();
    URL.revokeObjectURL(this.objectURL);
    this.video.remove();
    this.move("EVICTED", cause);
    this.listeners.clear();
    this.errorListeners.clear();
  }

  // ---- internals --------------------------------------------------------------------

  private move(to: PlayerState, cause: string, message?: string, error?: SessionError | null): void {
    this.machine.transition(to, cause);
    this.emit({ message, error });
  }

  /** emit publishes a new snapshot when something a view renders changed. */
  private emit(patch: { message?: string; error?: SessionError | null; poster?: string | null }): void {
    const prev = this.snapshot;
    const next: SessionSnapshot = {
      state: this.machine.state,
      message: patch.message ?? prev.message,
      error: patch.error === undefined ? prev.error : patch.error,
      poster: patch.poster === undefined ? prev.poster : patch.poster,
    };
    if (next.state === prev.state && next.message === prev.message && next.error === prev.error && next.poster === prev.poster) return;
    this.snapshot = next;
    this.listeners.forEach((l) => l());
  }

  private cancelPendingOpen(): void {
    clearTimeout(this.timer);
    this.cancelRetry?.();
    this.cancelRetry = undefined;
  }

  /** stop ends the stream with a permanent error: no automatic retry until retryNow(). */
  private stop(error: SessionError, cause: string, legacyMessage: string): void {
    if (!this.resilient) {
      this.move("ERROR", cause, legacyMessage);
      return;
    }
    this.stopped = true;
    this.captureFrame();
    this.cancelPendingOpen();
    this.dropSocket();
    this.move("ERROR", cause, error.label, error);
  }

  /** captureFrame keeps the current picture in memory (never persisted) as the poster for non-playing states. */
  private captureFrame(): void {
    if (!this.resilient || this.closed) return;
    const v = this.video;
    if (v.readyState < 2 || v.videoWidth === 0 || v.videoHeight === 0) return;
    try {
      const w = Math.min(FRAME_MAX_WIDTH, v.videoWidth);
      const h = Math.max(1, Math.round((w * v.videoHeight) / v.videoWidth));
      const canvas = (this.canvas ??= document.createElement("canvas"));
      canvas.width = w;
      canvas.height = h;
      const ctx = canvas.getContext("2d");
      if (!ctx) return;
      ctx.drawImage(v, 0, 0, w, h);
      canvas.toBlob(
        (blob) => {
          if (!blob || this.closed) return;
          const old = this.snapshot.poster;
          this.emit({ poster: URL.createObjectURL(blob) });
          if (old) URL.revokeObjectURL(old);
        },
        "image/jpeg",
        0.7,
      );
    } catch {
      // A frame that cannot be read just means no poster; the cold snapshot covers it.
    }
  }

  /** Moving a media element through the DOM pauses it; resume when it is still meant to play. */
  private onPause = (): void => {
    if (this.closed || this.video.ended || !this.video.isConnected || this.suspendReasons.size > 0) return;
    if (this.state === "ACTIVE" || this.state === "WARM") void this.video.play()?.catch(() => {});
  };

  private resumePlayback(): void {
    // Moving a media element through the DOM pauses it; resume, and jump to the live edge.
    if (!this.objectURL) return;
    const b = this.video.buffered;
    if (b.length > 0 && this.video.currentTime < b.end(b.length - 1) - 3) this.video.currentTime = b.end(b.length - 1) - 0.5;
    if (this.video.paused) void this.video.play()?.catch(() => {});
  }

  private dropSocket(): void {
    const ws = this.ws;
    this.ws = null;
    if (!ws) return;
    ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null;
    ws.close();
  }

  private onVisibility = (): void => {
    if (this.resilient) {
      this.setSuspended("page-hidden", document.visibilityState === "hidden");
      return;
    }
    if (document.visibilityState === "visible" && this.ws?.readyState !== WebSocket.OPEN && !this.closed) {
      clearTimeout(this.timer);
      this.open();
    }
  };

  private open(): void {
    if (this.closed) return;
    const { cameraId, quality, video: el } = this;
    const before = this.state;
    if (before === "UNINITIALIZED" || before === "ERROR" || before === "SUSPENDED" || before === "IDLE") {
      this.move("CONNECTING", "connect", "");
    } else if (before === "ACTIVE" || before === "WARM" || before === "BUFFERING") {
      this.move("RECONNECTING", "reopen", "");
    }
    const MS = mediaSourceCtor();
    if (!MS) {
      this.stop(streamError("mse_unsupported"), "mse-unsupported", "El navegador no soporta Media Source Extensions.");
      return;
    }
    this.dropSocket();
    this.metrics.connectAttempt(cameraId, quality);
    this.connectCount += 1;
    this.cancelFrame?.();
    this.cancelFrame = observeFirstFrame(el, () => this.metrics.firstFrame(cameraId, quality));

    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = this.createSocket(`${proto}//${location.host}/media/v1/cameras/${cameraId}/live?quality=${quality}`);
    this.ws = ws;
    ws.binaryType = "arraybuffer";
    let ms: MediaSource | null = null;
    let sb: SourceBuffer | null = null;
    const queue: ArrayBuffer[] = [];
    // The gateway rejects the handshake outright (no MSE upgrade) when the camera has
    // no go2rtc restream; the browser never exposes that HTTP status or body to us, so
    // "never opened" is the only signal we get for that permanent condition.
    let opened = false;
    let gotMedia = false;

    const pump = () => {
      if (!sb || sb.updating || queue.length === 0) return;
      try {
        sb.appendBuffer(queue.shift()!);
      } catch {
        // QuotaExceeded: drop the backlog, trimming below frees space
        queue.length = 0;
      }
    };

    ws.onopen = () => {
      opened = true;
      ws.send(JSON.stringify({ type: "mse", value: supportedCodecs(MS) }));
    };
    ws.onmessage = (ev) => {
      if (typeof ev.data === "string") {
        const msg = JSON.parse(ev.data) as { type: string; value: string; code?: string };
        if (msg.type === "mse") {
          this.move("BUFFERING", "mse-init");
          ms = new MS();
          ms.addEventListener(
            "sourceopen",
            () => {
              if (!ms) return;
              URL.revokeObjectURL(this.objectURL);
              try {
                sb = ms.addSourceBuffer(msg.value);
              } catch {
                this.stop(streamError("codec_unsupported"), "codec-unsupported", "Códec no soportado por el navegador.");
                return;
              }
              sb.mode = "segments";
              sb.addEventListener("updateend", () => {
                if (!sb || sb.updating) return;
                // Keep ~10 s of buffer and stay close to the live edge.
                const b = sb.buffered;
                if (b.length > 0) {
                  const end = b.end(b.length - 1);
                  const start = b.start(0);
                  if (end - start > 15 && !sb.updating) {
                    try {
                      sb.remove(start, end - 10);
                      return;
                    } catch {
                      // ignore; next update trims
                    }
                  }
                  if (el.currentTime < end - 3) el.currentTime = end - 0.5;
                }
                pump();
              });
              pump();
            },
            { once: true },
          );
          this.objectURL = URL.createObjectURL(ms);
          el.src = this.objectURL;
          void el.play()?.catch(() => {});
        } else if (msg.type === "error") {
          if (this.resilient) {
            const info = describeStreamError(msg);
            if (info.retryable) {
              // The gateway closes right after; onclose schedules the retry and keeps this label.
              this.captureFrame();
              this.move("RECONNECTING", "server-error", info.label, info);
            } else {
              this.stop(info, msg.code ?? "server-error", info.label);
            }
          } else {
            this.move("ERROR", "server-error", msg.value);
          }
          this.errorListeners.forEach((l) => l(msg.value));
        }
        return;
      }
      queue.push(ev.data as ArrayBuffer);
      if (queue.length > 60) queue.splice(0, queue.length - 60);
      pump();
      this.move(this.warm ? "WARM" : "ACTIVE", "media", "", null);
      this.retry = 0;
      if (!gotMedia) {
        gotMedia = true;
        this.backoff?.succeeded(this.serverId);
      }
    };
    ws.onclose = (ev) => {
      if (this.closed) return;
      if (this.resilient) {
        this.captureFrame();
        // 1008 is the gateway's policy-violation close for unauthorized/forbidden.
        if (ev?.code === 1008 && !this.snapshot.error) this.stop(streamError("unauthorized"), "close-1008", "");
        if (this.stopped) return;
      }
      if (!opened) {
        // Handshake rejected before any go2rtc protocol exchange: a permanent
        // condition (e.g. this camera has no go2rtc restream), not a dropped
        // connection. Show a clear message instead of retrying forever on "Conectando…".
        this.stop(streamError("handshake_rejected"), "handshake-rejected", "La cámara no tiene una transmisión de video disponible.");
        return;
      }
      const waiting = this.snapshot.error?.label ?? (this.retry >= 2 ? "Sin conexión con la cámara, reintentando…" : undefined);
      this.move("RECONNECTING", "socket-closed", waiting);
      this.retry = Math.min(this.retry + 1, 6);
      if (this.resilient && this.backoff) {
        this.cancelRetry = this.backoff.schedule(this.serverId, this.retry, () => {
          this.cancelRetry = undefined;
          this.open();
        });
      } else {
        this.timer = setTimeout(() => this.open(), reconnectDelay(this.retry, this.random));
      }
    };
  }
}
