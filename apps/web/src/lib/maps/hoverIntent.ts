export type HoverStage = "none" | "tooltip" | "snapshot" | "prewarm" | "live";

export interface HoverIntentState {
  cameraId: string | null;
  stage: HoverStage;
  x: number;
  y: number;
}

export interface HoverIntentOptions {
  onPrewarm?: (cameraId: string) => void;
  onRelease?: (cameraId: string) => void;
  liveOnHover?: boolean;
}

export class HoverIntentManager {
  private currentState: HoverIntentState = {
    cameraId: null,
    stage: "none",
    x: 0,
    y: 0,
  };

  private listeners = new Set<(state: HoverIntentState) => void>();
  private snapshotTimer: ReturnType<typeof setTimeout> | null = null;
  private prewarmTimer: ReturnType<typeof setTimeout> | null = null;
  private liveTimer: ReturnType<typeof setTimeout> | null = null;

  private onPrewarm?: (cameraId: string) => void;
  private onRelease?: (cameraId: string) => void;
  private liveOnHover: boolean;

  constructor(options: HoverIntentOptions = {}) {
    this.onPrewarm = options.onPrewarm;
    this.onRelease = options.onRelease;
    this.liveOnHover = options.liveOnHover ?? false;
  }

  setLiveOnHover(enabled: boolean): void {
    this.liveOnHover = enabled;
  }

  getState(): HoverIntentState {
    return this.currentState;
  }

  subscribe(listener: (state: HoverIntentState) => void): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  private notify(): void {
    for (const l of this.listeners) {
      try {
        l(this.currentState);
      } catch {
        // Ignore listener error
      }
    }
  }

  private clearTimers(): void {
    if (this.snapshotTimer) {
      clearTimeout(this.snapshotTimer);
      this.snapshotTimer = null;
    }
    if (this.prewarmTimer) {
      clearTimeout(this.prewarmTimer);
      this.prewarmTimer = null;
    }
    if (this.liveTimer) {
      clearTimeout(this.liveTimer);
      this.liveTimer = null;
    }
  }

  enter(cameraId: string, x = 0, y = 0): void {
    if (this.currentState.cameraId === cameraId && this.currentState.stage !== "none") {
      this.currentState = { ...this.currentState, x, y };
      this.notify();
      return;
    }

    if (this.currentState.cameraId && this.currentState.cameraId !== cameraId) {
      this.onRelease?.(this.currentState.cameraId);
    }

    this.clearTimers();

    // 0ms: Tooltip stage
    this.currentState = {
      cameraId,
      stage: "tooltip",
      x,
      y,
    };
    this.notify();

    // 150ms: Snapshot stage
    this.snapshotTimer = setTimeout(() => {
      this.currentState = { ...this.currentState, stage: "snapshot" };
      this.notify();
    }, 150);

    // 400ms: Prewarm stage
    this.prewarmTimer = setTimeout(() => {
      this.currentState = { ...this.currentState, stage: "prewarm" };
      this.onPrewarm?.(cameraId);
      this.notify();
    }, 400);

    // 700ms: Live video stage (if liveOnHover enabled)
    if (this.liveOnHover) {
      this.liveTimer = setTimeout(() => {
        this.currentState = { ...this.currentState, stage: "live" };
        this.notify();
      }, 700);
    }
  }

  updatePosition(x: number, y: number): void {
    if (this.currentState.stage === "none") return;
    this.currentState = { ...this.currentState, x, y };
    this.notify();
  }

  leave(): void {
    if (this.currentState.cameraId) {
      this.onRelease?.(this.currentState.cameraId);
    }
    this.clearTimers();
    this.currentState = {
      cameraId: null,
      stage: "none",
      x: 0,
      y: 0,
    };
    this.notify();
  }

  destroy(): void {
    this.leave();
    this.listeners.clear();
  }
}
