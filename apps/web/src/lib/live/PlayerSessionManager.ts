import { PlayerSession, type SessionQuality } from "./PlayerSession";

export type PlayerSessionManagerOptions = {
  /** How long a released session keeps streaming (WARM) before it is evicted. Default 30 s. */
  warmSessionTTL?: number;
  /** Most WARM sessions kept at once; the least recently released are evicted first. Default 8. */
  maxWarmPlayers?: number;
  /**
   * Soft cap on live sessions. Creating a session at the cap evicts WARM sessions (oldest
   * first); sessions that are in use are never evicted, so the cap can be exceeded by them.
   */
  maxConcurrentPlayers?: number;
  /** Injectable for tests; defaults to a real PlayerSession that is connected immediately. */
  createSession?: (cameraId: string, quality: SessionQuality) => PlayerSession;
};

export const DEFAULT_WARM_SESSION_TTL_MS = 30_000;
export const DEFAULT_MAX_WARM_PLAYERS = 8;
export const DEFAULT_MAX_CONCURRENT_PLAYERS = 32;

type Entry = { session: PlayerSession; refs: number; timer?: ReturnType<typeof setTimeout>; releasedAt: number };

const keyOf = (cameraId: string, quality: SessionQuality) => `${cameraId}:${quality}`;

/**
 * PlayerSessionManager keeps one PlayerSession per camera+quality alive across UI changes.
 * acquire() returns the existing session (cancelling any pending eviction) or creates one;
 * release() moves it to WARM and evicts it after `warmSessionTTL`, so leaving Live and coming
 * back shortly after reuses the running stream. Everything is dropped by clear() on logout.
 * Frames and pipelines live in memory only; nothing is persisted.
 */
export class PlayerSessionManager {
  private readonly entries = new Map<string, Entry>();
  private readonly warmSessionTTL: number;
  private readonly maxWarmPlayers: number;
  private readonly maxConcurrentPlayers: number;
  private readonly createSession: (cameraId: string, quality: SessionQuality) => PlayerSession;
  private releaseSeq = 0;

  constructor(opts: PlayerSessionManagerOptions = {}) {
    this.warmSessionTTL = opts.warmSessionTTL ?? DEFAULT_WARM_SESSION_TTL_MS;
    this.maxWarmPlayers = opts.maxWarmPlayers ?? DEFAULT_MAX_WARM_PLAYERS;
    this.maxConcurrentPlayers = opts.maxConcurrentPlayers ?? DEFAULT_MAX_CONCURRENT_PLAYERS;
    this.createSession =
      opts.createSession ??
      ((cameraId, quality) => {
        const s = new PlayerSession({ cameraId, quality });
        s.connect();
        return s;
      });
  }

  /** acquire returns the live session for the camera, creating it when needed. Pair with release(). */
  acquire(cameraId: string, quality: SessionQuality): PlayerSession {
    const key = keyOf(cameraId, quality);
    let entry = this.entries.get(key);
    if (entry) {
      clearTimeout(entry.timer);
      entry.timer = undefined;
      entry.refs += 1;
      entry.session.markActive("acquired");
      return entry.session;
    }
    this.makeRoom();
    entry = { session: this.createSession(cameraId, quality), refs: 1, releasedAt: 0 };
    this.entries.set(key, entry);
    return entry.session;
  }

  /** release gives back one acquisition; the last one turns the session WARM and starts its TTL. */
  release(cameraId: string, quality: SessionQuality): void {
    const key = keyOf(cameraId, quality);
    const entry = this.entries.get(key);
    if (!entry || entry.refs === 0) return;
    entry.refs -= 1;
    if (entry.refs > 0) return;
    entry.releasedAt = ++this.releaseSeq;
    entry.session.markWarm("released");
    entry.timer = setTimeout(() => this.evict(key, "warm-ttl"), this.warmSessionTTL);
    this.trimWarm();
  }

  /** clear closes every session (logout or user change). */
  clear(): void {
    for (const key of [...this.entries.keys()]) this.evict(key, "cleared");
  }

  /** stats reports counts for diagnostics. */
  stats(): { total: number; active: number; warm: number } {
    let warm = 0;
    for (const e of this.entries.values()) if (e.refs === 0) warm += 1;
    return { total: this.entries.size, active: this.entries.size - warm, warm };
  }

  private evict(key: string, cause: string): void {
    const entry = this.entries.get(key);
    if (!entry) return;
    clearTimeout(entry.timer);
    this.entries.delete(key);
    entry.session.close(cause);
  }

  /** warmKeysOldestFirst lists released sessions, least recently released first (LRU). */
  private warmKeysOldestFirst(): string[] {
    return [...this.entries.entries()]
      .filter(([, e]) => e.refs === 0)
      .sort(([, a], [, b]) => a.releasedAt - b.releasedAt)
      .map(([k]) => k);
  }

  private trimWarm(): void {
    const warm = this.warmKeysOldestFirst();
    for (const key of warm.slice(0, Math.max(0, warm.length - this.maxWarmPlayers))) this.evict(key, "max-warm");
  }

  private makeRoom(): void {
    const warm = this.warmKeysOldestFirst();
    let over = this.entries.size + 1 - this.maxConcurrentPlayers;
    for (const key of warm) {
      if (over <= 0) break;
      this.evict(key, "max-concurrent");
      over -= 1;
    }
  }
}
