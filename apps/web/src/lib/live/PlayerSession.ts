import { reconnectDelay } from "./backoff";
import { PlayerStateMachine, type PlayerState, type StateTransition } from "./playerState";
import { observeFirstFrame, playerMetrics, type PlayerMetrics } from "./playerMetrics";

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

/** What a view needs to render a session: the lifecycle state and a user-facing message. */
export type SessionSnapshot = { readonly state: PlayerState; readonly message: string };

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
  private snapshot: SessionSnapshot = { state: "UNINITIALIZED", message: "" };

  private ws: WebSocket | null = null;
  private closed = false;
  private started = false;
  private warm = false;
  private retry = 0;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private objectURL = "";
  private cancelFrame: (() => void) | undefined;
  private connectCount = 0;

  constructor(opts: PlayerSessionOptions) {
    this.cameraId = opts.cameraId;
    this.quality = opts.quality;
    this.metrics = opts.metrics ?? playerMetrics;
    this.createSocket = opts.createSocket ?? ((url) => new WebSocket(url));
    this.random = opts.random ?? Math.random;
    if (opts.onError) this.errorListeners.add(opts.onError);
    this.machine = new PlayerStateMachine({
      cameraId: opts.cameraId,
      quality: opts.quality,
      now: opts.now,
      metrics: this.metrics,
    });
    this.video = document.createElement("video");
    this.video.className = "size-full object-contain";
    this.video.autoplay = true;
    this.video.playsInline = true;
    this.setMuted(opts.muted ?? true);
  }

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
    this.open();
  }

  /** attach places the persistent `<video>` in `container`, keeping the pipeline running. */
  attach(container: HTMLElement): void {
    if (this.closed) return;
    if (this.video.parentElement !== container) container.appendChild(this.video);
    this.resumePlayback();
  }

  /** detach removes the `<video>` from the DOM without closing anything. */
  detach(): void {
    this.video.remove();
  }

  setMuted(muted: boolean): void {
    this.video.muted = muted;
    if (muted) this.video.setAttribute("muted", "");
    else this.video.removeAttribute("muted");
  }

  /** markWarm flags the session as running without a visible view (kept for a quick return). */
  markWarm(cause = "released"): void {
    if (this.closed) return;
    this.warm = true;
    this.move("WARM", cause);
  }

  /** markActive flags the session as being watched again. */
  markActive(cause = "acquired"): void {
    if (this.closed) return;
    this.warm = false;
    this.move("ACTIVE", cause);
  }

  /** close releases the socket, the media pipeline and the `<video>`; the session is unusable afterwards. */
  close(cause = "closed"): void {
    if (this.closed) return;
    this.closed = true;
    clearTimeout(this.timer);
    this.cancelFrame?.();
    document.removeEventListener("visibilitychange", this.onVisibility);
    this.dropSocket();
    this.video.removeAttribute("src");
    this.video.load();
    URL.revokeObjectURL(this.objectURL);
    this.video.remove();
    this.move("EVICTED", cause);
    this.listeners.clear();
    this.errorListeners.clear();
  }

  // ---- internals --------------------------------------------------------------------

  private move(to: PlayerState, cause: string, message?: string): void {
    const moved = this.machine.transition(to, cause);
    const nextMessage = message ?? this.snapshot.message;
    if (!moved && nextMessage === this.snapshot.message) return;
    this.snapshot = { state: this.machine.state, message: nextMessage };
    this.listeners.forEach((l) => l());
  }

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
      this.move("ERROR", "mse-unsupported", "El navegador no soporta Media Source Extensions.");
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
        const msg = JSON.parse(ev.data) as { type: string; value: string };
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
                this.move("ERROR", "codec-unsupported", "Códec no soportado por el navegador.");
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
          this.move("ERROR", "server-error", msg.value);
          this.errorListeners.forEach((l) => l(msg.value));
        }
        return;
      }
      queue.push(ev.data as ArrayBuffer);
      if (queue.length > 60) queue.splice(0, queue.length - 60);
      pump();
      this.move(this.warm ? "WARM" : "ACTIVE", "media", "");
      this.retry = 0;
    };
    ws.onclose = () => {
      if (this.closed) return;
      if (!opened) {
        // Handshake rejected before any go2rtc protocol exchange: a permanent
        // condition (e.g. this camera has no go2rtc restream), not a dropped
        // connection. Show a clear message instead of retrying forever on "Conectando…".
        this.move("ERROR", "handshake-rejected", "La cámara no tiene una transmisión de video disponible.");
        return;
      }
      this.move("RECONNECTING", "socket-closed", this.retry >= 2 ? "Sin conexión con la cámara, reintentando…" : undefined);
      this.retry = Math.min(this.retry + 1, 6);
      this.timer = setTimeout(() => this.open(), reconnectDelay(this.retry, this.random));
    };
  }
}
