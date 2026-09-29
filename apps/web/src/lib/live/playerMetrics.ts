/**
 * playerMetrics records per camera+quality session diagnostics for Live View: connect
 * attempts, reconnect count, time to first frame, current state and the state transition
 * log. It is framework-free; in development (and tests) the shared instance is exposed as
 * `window.__openvmsPlayerMetrics` so diagnostics and tests can read it.
 */

export type SessionMetrics = {
  key: string;
  cameraId: string;
  quality: string;
  state: string;
  connectAttempts: number;
  /** Connect attempts beyond the first one for this camera+quality. */
  reconnectCount: number;
  /** Milliseconds from the latest connect attempt to its first rendered frame. */
  ttffMs: number | null;
  transitions: TransitionRecord[];
};

export type TransitionRecord = { from: string; to: string; cause: string; at: number };

type Internal = SessionMetrics & { attemptAt: number | null };

const MAX_TRANSITIONS = 50;

export function createPlayerMetrics(clock: () => number = () => performance.now()) {
  const sessions = new Map<string, Internal>();
  const listeners = new Set<() => void>();

  const notify = () => listeners.forEach((l) => l());
  const entry = (cameraId: string, quality: string): Internal => {
    const key = `${cameraId}:${quality}`;
    let s = sessions.get(key);
    if (!s) {
      s = { key, cameraId, quality, state: "UNINITIALIZED", connectAttempts: 0, reconnectCount: 0, ttffMs: null, transitions: [], attemptAt: null };
      sessions.set(key, s);
    }
    return s;
  };
  const publicView = (s: Internal): SessionMetrics => ({
    key: s.key,
    cameraId: s.cameraId,
    quality: s.quality,
    state: s.state,
    connectAttempts: s.connectAttempts,
    reconnectCount: s.reconnectCount,
    ttffMs: s.ttffMs,
    transitions: [...s.transitions],
  });

  return {
    connectAttempt(cameraId: string, quality: string) {
      const s = entry(cameraId, quality);
      s.connectAttempts += 1;
      s.reconnectCount = s.connectAttempts - 1;
      s.attemptAt = clock();
      notify();
    },
    firstFrame(cameraId: string, quality: string) {
      const s = entry(cameraId, quality);
      if (s.attemptAt === null) return;
      s.ttffMs = clock() - s.attemptAt;
      s.attemptAt = null;
      notify();
    },
    setState(cameraId: string, quality: string, state: string) {
      entry(cameraId, quality).state = state;
      notify();
    },
    transition(cameraId: string, quality: string, t: Omit<TransitionRecord, "at">) {
      const s = entry(cameraId, quality);
      s.state = t.to;
      s.transitions.push({ ...t, at: clock() });
      if (s.transitions.length > MAX_TRANSITIONS) s.transitions.shift();
      notify();
    },
    get(cameraId: string, quality: string): SessionMetrics | undefined {
      const s = sessions.get(`${cameraId}:${quality}`);
      return s ? publicView(s) : undefined;
    },
    snapshot(): SessionMetrics[] {
      return [...sessions.values()].map(publicView);
    },
    reset() {
      sessions.clear();
      notify();
    },
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}

export type PlayerMetrics = ReturnType<typeof createPlayerMetrics>;

/** The shared instance every player reports to. */
export const playerMetrics: PlayerMetrics = createPlayerMetrics();

declare global {
  interface Window {
    __openvmsPlayerMetrics?: PlayerMetrics;
  }
}

if (import.meta.env.DEV && typeof window !== "undefined") {
  window.__openvmsPlayerMetrics = playerMetrics;
}

type FrameVideo = HTMLVideoElement & { requestVideoFrameCallback?: (cb: () => void) => number; cancelVideoFrameCallback?: (id: number) => void };

/**
 * observeFirstFrame calls `cb` once when `video` renders its first frame: via
 * requestVideoFrameCallback where supported, otherwise the `loadeddata` event. Returns a cancel function.
 */
export function observeFirstFrame(video: HTMLVideoElement, cb: () => void): () => void {
  const v = video as FrameVideo;
  let done = false;
  const fire = () => {
    if (done) return;
    done = true;
    v.removeEventListener("loadeddata", fire);
    cb();
  };
  if (typeof v.requestVideoFrameCallback === "function") {
    const id = v.requestVideoFrameCallback(fire);
    return () => {
      done = true;
      v.cancelVideoFrameCallback?.(id);
    };
  }
  v.addEventListener("loadeddata", fire);
  return () => {
    done = true;
    v.removeEventListener("loadeddata", fire);
  };
}
