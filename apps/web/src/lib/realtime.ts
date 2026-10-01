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
  "server.status_changed": [["servers"], ["health"], ["cameras"]],
  "alarm.updated": [["alarms"]],
  "alarm.created": [["alarms"]],
  "alarm.acknowledged": [["alarms"]],
  "notification.created": [["notifications"]],
  "camera.status_changed": [["cameras"], ["health"]],
};
const catchUpKeys = [...new Set(Object.values(invalidationsByType).flat().map((k) => JSON.stringify(k)))].map((k) => JSON.parse(k) as string[]);

export type FrameListener = (frame: unknown) => void;
const frameListeners = new Set<FrameListener>();

/**
 * Subscribe to raw incoming realtime frames (individual envelopes, batch envelopes, or control frames).
 * Returns an unsubscribe cleanup function.
 */
export function subscribeFrames(listener: FrameListener): () => void {
  frameListeners.add(listener);
  return () => {
    frameListeners.delete(listener);
  };
}

function notifyFrameListeners(frame: unknown) {
  for (const listener of frameListeners) {
    try {
      listener(frame);
    } catch {
      // Listener error should not affect others
    }
  }
}

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

        // Send hello control frame to server per protocol (M-B6)
        try {
          const hello = JSON.stringify({
            op: "hello",
            v: 2,
            topics: ["events", "servers", "alarms", "notifications", "cameras"],
          });
          ws.send(hello);
        } catch {
          // Ignore send failures if socket closed prematurely
        }

        // No replay: catch up once per (re)connect.
        invalidate(qc, catchUpKeys);
        stableTimer = setTimeout(() => (attempt = 0), STABLE_AFTER_MS);
      };
      ws.onmessage = (e) => {
        try {
          const raw = JSON.parse(String(e.data)) as {
            op?: string;
            frames?: unknown[];
            type?: unknown;
            streams?: string[];
          };
          if (!raw || typeof raw !== "object") return;

          if (raw.op === "batch" && Array.isArray(raw.frames)) {
            for (const subFrame of raw.frames) {
              notifyFrameListeners(subFrame);
              if (subFrame && typeof subFrame === "object" && "type" in subFrame) {
                const keys = typeof subFrame.type === "string" ? invalidationsByType[subFrame.type] : undefined;
                if (keys) invalidate(qc, keys);
              }
            }
          } else if (raw.op === "resync") {
            notifyFrameListeners(raw);
            invalidate(qc, catchUpKeys);
          } else {
            notifyFrameListeners(raw);
            const keys = typeof raw.type === "string" ? invalidationsByType[raw.type] : undefined;
            if (keys) invalidate(qc, keys);
          }
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
