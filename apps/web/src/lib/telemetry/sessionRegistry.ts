import type { StreamStatsSource } from "./types";

/**
 * Framework-free set of live stream sources. Players register themselves on construction and
 * unregister on close, so telemetry can enumerate every live stream, including players that
 * are not owned by a session manager.
 */
export type SessionRegistry = {
  register(source: StreamStatsSource): void;
  unregister(source: StreamStatsSource): void;
  /** A snapshot copy, in registration order. */
  list(): readonly StreamStatsSource[];
};

export function createSessionRegistry(): SessionRegistry {
  const sources = new Set<StreamStatsSource>();
  return {
    register: (source) => void sources.add(source),
    unregister: (source) => void sources.delete(source),
    list: () => Array.from(sources),
  };
}

/** Process-wide registry used by default by every player session. */
export const sessionRegistry: SessionRegistry = createSessionRegistry();
