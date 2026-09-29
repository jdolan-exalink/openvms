import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

/**
 * App-wide push feed. One `/ws` connection tells the UI *what* changed; REST stays the
 * source of truth, so every frame only invalidates React Query caches.
 *
 * Polling decision: the existing `refetchInterval`s are left unchanged. They keep working
 * while the socket is down (the safe fallback) and act as a cheap safety net while it is
 * up, since the feed has no replay. `connected` is exposed for a future indicator.
 */

/** Query-key prefixes to refresh per frame type. Unknown types are ignored. */
const invalidationsByType: Record<string, readonly (readonly string[])[]> = {
  "event.created": [["events"], ["plates"]],
  "server.status": [["servers"], ["health"], ["cameras"]],
};
const catchUpKeys = [...new Set(Object.values(invalidationsByType).flat().map((k) => JSON.stringify(k)))].map((k) => JSON.parse(k) as string[]);

const BASE_DELAY_MS = 1_000;
const MAX_DELAY_MS = 30_000;
const STABLE_AFTER_MS = 10_000;

export interface RealtimeOptions {
  enabled?: boolean;
  /** Jitter source in [0, 1); injectable for tests. */
  random?: () => number;
}

export function backoffDelay(attempt: number, random: () => number) {
  const ceiling = Math.min(MAX_DELAY_MS, BASE_DELAY_MS * 2 ** attempt);
  return Math.round(ceiling * (0.5 + 0.5 * random()));
}

function socketUrl() {
  return `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws`;
}

function invalidate(qc: QueryClient, keys: readonly (readonly string[])[]) {
  for (const queryKey of keys) void qc.invalidateQueries({ queryKey: [...queryKey] });
}

export function useRealtimeFeed({ enabled = true, random = Math.random }: RealtimeOptions = {}) {
  const qc = useQueryClient();
  const [connected, setConnected] = useState(false);
  const randomRef = useRef(random);
  useEffect(() => {
    randomRef.current = random;
  }, [random]);

  useEffect(() => {
    if (!enabled) return;
    let stopped = false;
    let attempt = 0;
    let socket: WebSocket | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;
    let stableTimer: ReturnType<typeof setTimeout> | undefined;

    const connect = () => {
      const ws = new WebSocket(socketUrl());
      socket = ws;
      ws.onopen = () => {
        if (stopped) return;
        setConnected(true);
        // No replay: catch up once per (re)connect.
        invalidate(qc, catchUpKeys);
        stableTimer = setTimeout(() => (attempt = 0), STABLE_AFTER_MS);
      };
      ws.onmessage = (e) => {
        try {
          const frame = JSON.parse(String(e.data)) as { type?: unknown };
          const keys = typeof frame.type === "string" ? invalidationsByType[frame.type] : undefined;
          if (keys) invalidate(qc, keys);
        } catch {
          // malformed frame: ignore
        }
      };
      ws.onclose = (e) => {
        clearTimeout(stableTimer);
        if (stopped) return;
        setConnected(false);
        if (e.code === 1008 && e.reason === "session ended") {
          // The session is gone: do not reconnect. Refetching /me hits REST 401 -> login.
          invalidate(qc, [["me"]]);
          return;
        }
        retryTimer = setTimeout(connect, backoffDelay(attempt++, randomRef.current));
      };
    };

    connect();
    return () => {
      stopped = true;
      clearTimeout(retryTimer);
      clearTimeout(stableTimer);
      if (socket) socket.close();
      setConnected(false);
    };
  }, [enabled, qc]);

  return { connected };
}
