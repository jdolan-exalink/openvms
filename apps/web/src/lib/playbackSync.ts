/**
 * Drift correction for synchronized multi-camera playback (S2-8).
 *
 * The primary ("master") player's currentTime is the reference clock. Every player of a
 * grid plays the same wall-clock VOD window, so the same currentTime means the same instant.
 * decideSync is pure: it maps (master time, follower state) to one corrective action.
 */

/** Drift (seconds) under which a follower is considered in sync. */
export const DRIFT_TOLERANCE_S = 0.08;
/** While nudging, keep correcting until drift falls under this (hysteresis avoids flapping). */
export const DRIFT_SETTLED_S = 0.04;
/** Drift (seconds) above which nudging is too slow and a hard seek is used. */
export const DRIFT_HARD_SEEK_S = 1;
/** Relative playbackRate change used to catch up or fall back (5 %). */
export const NUDGE_FACTOR = 0.05;
/** Interval of the sync loop in milliseconds. */
export const SYNC_INTERVAL_MS = 300;
/** HTMLMediaElement.HAVE_FUTURE_DATA: below this the follower is buffering. */
export const READY_STATE_PLAYABLE = 3;

export type FollowerState = {
  currentTime: number;
  playbackRate: number;
  paused: boolean;
  seeking: boolean;
  readyState: number;
  /** True when the player has no recording / failed to load (no media to correct). */
  unavailable?: boolean;
};

export type SyncAction =
  | { kind: "none" }
  | { kind: "rate"; rate: number }
  | { kind: "seek"; time: number };

const EPS = 1e-6;

/**
 * decideSync returns the corrective action for one follower.
 * speed is the user-selected global playback speed (the master's playbackRate); nudging is
 * multiplied around it, never around 1.0.
 */
export function decideSync(masterTime: number, speed: number, f: FollowerState): SyncAction {
  // Buffering, seeking, paused or without media: do not fight the player; resync once it plays.
  if (f.unavailable || f.seeking || f.paused || f.readyState < READY_STATE_PLAYABLE) {
    return Math.abs(f.playbackRate - speed) > EPS ? { kind: "rate", rate: speed } : { kind: "none" };
  }
  const drift = f.currentTime - masterTime; // > 0: follower is ahead
  const abs = Math.abs(drift);
  if (abs > DRIFT_HARD_SEEK_S) return { kind: "seek", time: masterTime };

  const nudging = Math.abs(f.playbackRate - speed) > EPS;
  const threshold = nudging ? DRIFT_SETTLED_S : DRIFT_TOLERANCE_S;
  if (abs <= threshold) return nudging ? { kind: "rate", rate: speed } : { kind: "none" };

  const rate = speed * (drift > 0 ? 1 - NUDGE_FACTOR : 1 + NUDGE_FACTOR);
  return Math.abs(f.playbackRate - rate) > EPS ? { kind: "rate", rate } : { kind: "none" };
}
