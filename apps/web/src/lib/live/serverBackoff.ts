import { reconnectDelay } from "./backoff";

type Waiter = { run: () => void; attempt: number; release?: ReturnType<typeof setTimeout> };

type Group = {
  /** Backoff level shared by the group; reset when any session of the server gets media. */
  level: number;
  queue: Waiter[];
  timer?: ReturnType<typeof setTimeout>;
  probing: Waiter | null;
  probeTimer?: ReturnType<typeof setTimeout>;
};

export type ServerBackoffOptions = {
  random?: () => number;
  baseMs?: number;
  maxMs?: number;
  /** How long the probing session may take to get media before the group backs off again. */
  probeTimeoutMs?: number;
  /** Spread between sessions released after a successful probe. */
  staggerMs?: number;
};

/**
 * ServerBackoff coordinates reconnects per Frigate server so that when a server drops, its N
 * sessions do not all retry in lockstep (a reconnect storm). Failures within the same group
 * share one backoff timer; when it fires a single session probes the server and the rest stay
 * queued until the probe gets media, then they are released with a small stagger. Sessions with
 * no server id, or a lone failing session, behave like a plain jittered exponential backoff.
 */
export class ServerBackoff {
  private readonly groups = new Map<string, Group>();
  private readonly random: () => number;
  private readonly baseMs: number;
  private readonly maxMs: number;
  private readonly probeTimeoutMs: number;
  private readonly staggerMs: number;

  constructor(opts: ServerBackoffOptions = {}) {
    this.random = opts.random ?? Math.random;
    this.baseMs = opts.baseMs ?? 500;
    this.maxMs = opts.maxMs ?? 32_000;
    this.probeTimeoutMs = opts.probeTimeoutMs ?? 8_000;
    this.staggerMs = opts.staggerMs ?? 150;
  }

  /** schedule asks for `run` to be called once the (group) backoff allows; returns a cancel function. */
  schedule(serverId: string | undefined, attempt: number, run: () => void): () => void {
    if (!serverId) {
      const t = setTimeout(run, this.delay(attempt));
      return () => clearTimeout(t);
    }
    const g = this.group(serverId);
    // A new failure while a probe is in flight means the server is still unhealthy.
    if (g.probing) {
      clearTimeout(g.probeTimer);
      g.probing = null;
    }
    const w: Waiter = { run, attempt };
    g.queue.push(w);
    g.level = Math.max(g.level, attempt);
    if (!g.timer) this.arm(g);
    return () => this.cancel(g, w);
  }

  /** succeeded reports media from a session of the server: the group recovers and its queue is released. */
  succeeded(serverId: string | undefined): void {
    const g = serverId ? this.groups.get(serverId) : undefined;
    if (!g) return;
    clearTimeout(g.probeTimer);
    clearTimeout(g.timer);
    g.timer = undefined;
    g.probing = null;
    g.level = 0;
    const queue = g.queue;
    g.queue = [];
    queue.forEach((w, i) => {
      w.release = setTimeout(w.run, i * this.staggerMs + this.random() * this.staggerMs);
    });
  }

  /** clear drops every pending timer (logout). */
  clear(): void {
    for (const g of this.groups.values()) {
      clearTimeout(g.timer);
      clearTimeout(g.probeTimer);
      g.queue.forEach((w) => clearTimeout(w.release));
    }
    this.groups.clear();
  }

  private delay(attempt: number): number {
    return reconnectDelay(attempt, this.random, { baseMs: this.baseMs, maxMs: this.maxMs });
  }

  private group(serverId: string): Group {
    let g = this.groups.get(serverId);
    if (!g) {
      g = { level: 0, queue: [], probing: null };
      this.groups.set(serverId, g);
    }
    return g;
  }

  private arm(g: Group): void {
    g.timer = setTimeout(() => this.fire(g), this.delay(g.level));
  }

  private fire(g: Group): void {
    g.timer = undefined;
    const probe = g.queue.shift();
    if (!probe) return;
    g.probing = probe;
    g.probeTimer = setTimeout(() => {
      g.probing = null;
      if (g.queue.length > 0) {
        g.level = Math.min(g.level + 1, 6);
        this.arm(g);
      }
    }, this.probeTimeoutMs);
    probe.run();
  }

  private cancel(g: Group, w: Waiter): void {
    clearTimeout(w.release);
    g.queue = g.queue.filter((x) => x !== w);
    if (g.queue.length === 0) {
      clearTimeout(g.timer);
      g.timer = undefined;
    }
  }
}
