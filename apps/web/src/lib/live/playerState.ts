import { playerMetrics, type PlayerMetrics } from "./playerMetrics";

/**
 * Lifecycle of one camera player session. Pure TypeScript (no React, no DOM) so the rules are
 * easy to reason about and to test.
 *
 *  UNINITIALIZED  created, nothing opened yet
 *  CONNECTING     opening the transport (first attempt)
 *  BUFFERING      transport open, waiting for the first media
 *  ACTIVE         media flowing and visible
 *  WARM           media flowing but no view is attached (kept for a quick return)
 *  IDLE           connected but paused by policy (no data expected)
 *  SUSPENDED      transport closed on purpose (policy, hidden tab); can resume
 *  RECONNECTING   transport dropped, backoff timer running or reopening
 *  ERROR          permanent failure until someone retries
 *  EVICTED        terminal: resources released
 */
export const PLAYER_STATES = [
  "UNINITIALIZED",
  "CONNECTING",
  "BUFFERING",
  "ACTIVE",
  "WARM",
  "IDLE",
  "SUSPENDED",
  "RECONNECTING",
  "ERROR",
  "EVICTED",
] as const;

export type PlayerState = (typeof PLAYER_STATES)[number];

export const ALLOWED_TRANSITIONS: Readonly<Record<PlayerState, readonly PlayerState[]>> = {
  UNINITIALIZED: ["CONNECTING", "ERROR", "EVICTED"],
  CONNECTING: ["BUFFERING", "ACTIVE", "WARM", "RECONNECTING", "SUSPENDED", "ERROR", "EVICTED"],
  BUFFERING: ["ACTIVE", "WARM", "RECONNECTING", "SUSPENDED", "ERROR", "EVICTED"],
  ACTIVE: ["BUFFERING", "WARM", "IDLE", "SUSPENDED", "RECONNECTING", "ERROR", "EVICTED"],
  WARM: ["ACTIVE", "BUFFERING", "IDLE", "SUSPENDED", "RECONNECTING", "ERROR", "EVICTED"],
  IDLE: ["ACTIVE", "WARM", "CONNECTING", "SUSPENDED", "RECONNECTING", "EVICTED"],
  SUSPENDED: ["CONNECTING", "ACTIVE", "WARM", "EVICTED"],
  RECONNECTING: ["CONNECTING", "BUFFERING", "ACTIVE", "WARM", "SUSPENDED", "ERROR", "EVICTED"],
  ERROR: ["CONNECTING", "RECONNECTING", "EVICTED"],
  EVICTED: [],
};

export function canTransition(from: PlayerState, to: PlayerState): boolean {
  return ALLOWED_TRANSITIONS[from].includes(to);
}

export type StateTransition = { from: PlayerState; to: PlayerState; cause: string; at: number };

/** localStorage key that turns on console logging of every player transition. */
export const PLAYER_DEBUG_KEY = "openvms.live.debug";

function debugEnabled(): boolean {
  try {
    return typeof localStorage !== "undefined" && localStorage.getItem(PLAYER_DEBUG_KEY) === "1";
  } catch {
    return false;
  }
}

const HISTORY_LIMIT = 100;

export type PlayerStateMachineOptions = {
  cameraId: string;
  quality: string;
  now?: () => number;
  metrics?: PlayerMetrics;
  onChange?: (t: StateTransition) => void;
};

/** PlayerStateMachine enforces the allowed transitions and records each one with its cause. */
export class PlayerStateMachine {
  private current: PlayerState = "UNINITIALIZED";
  private log: StateTransition[] = [];
  private readonly now: () => number;
  private readonly metrics: PlayerMetrics;

  constructor(private readonly opts: PlayerStateMachineOptions) {
    this.now = opts.now ?? (() => Date.now());
    this.metrics = opts.metrics ?? playerMetrics;
  }

  get state(): PlayerState {
    return this.current;
  }

  get history(): readonly StateTransition[] {
    return this.log;
  }

  /**
   * transition moves to `to` recording `cause`. Returns false, changing nothing, when the
   * target equals the current state or the move is not allowed (logged when debugging is on).
   */
  transition(to: PlayerState, cause: string): boolean {
    const from = this.current;
    if (from === to) return false;
    if (!canTransition(from, to)) {
      if (debugEnabled()) console.debug(`[player ${this.opts.cameraId}:${this.opts.quality}] rejected ${from} -> ${to} (${cause})`);
      return false;
    }
    const t: StateTransition = { from, to, cause, at: this.now() };
    this.current = to;
    this.log.push(t);
    if (this.log.length > HISTORY_LIMIT) this.log.shift();
    this.metrics.transition(this.opts.cameraId, this.opts.quality, { from, to, cause });
    if (debugEnabled()) console.debug(`[player ${this.opts.cameraId}:${this.opts.quality}] ${from} -> ${to} (${cause})`);
    this.opts.onChange?.(t);
    return true;
  }
}
