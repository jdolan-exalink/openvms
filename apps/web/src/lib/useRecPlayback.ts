import { useCallback, useEffect, useRef, useState } from "react";
import type { HlsPlayerHandle } from "@/components/HlsPlayer";
import { startOfLocalDay } from "@/lib/liveRec";
import { vodWindowForInstant } from "@/lib/recordings";

type Win = { start: number; end: number; offset: number };

/** Seeks this close to the end of the current VOD window open a new window instead. */
const WINDOW_MARGIN_S = 10;
/** Playing within this many seconds of the window end asks for the next window. */
const ROLLOVER_MARGIN_S = 4;

const unixNow = () => Math.floor(Date.now() / 1000);

function makeWin(t: number, now: number): Win {
  const { start, end } = vodWindowForInstant(t, startOfLocalDay(t), now);
  return { start, end, offset: t - start };
}

/** PositionStore holds the shared clock outside React state so ~4 Hz updates only re-render subscribers. */
class PositionStore {
  private listeners = new Set<() => void>();
  constructor(private value: number) {}
  get = () => this.value;
  set(value: number) {
    if (value === this.value) return;
    this.value = value;
    this.listeners.forEach((l) => l());
  }
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
}

/** Detection whose snapshot stands in for its camera's tile while the recording loads. */
export type RecPoster = { cameraId: string; eventId: string; key: number };

export type RecTransport = ReturnType<typeof useRecPlayback>;

/**
 * useRecPlayback drives the synchronized recorded grid of the Live REC mode. All tiles play
 * the same wall-clock VOD window, so one currentTime means one instant; the master tile's
 * clock is the shared position (the caller runs useSyncedPlayback, S2-8, to correct drift).
 * Seeks inside the loaded window only move currentTime; outside it a new window is opened
 * (players remount through `win.start`). Inert while `active` is false.
 */
export function useRecPlayback({ active, seedT, now }: { active: boolean; seedT: number; now: number }) {
  const [win, setWin] = useState<Win>(() => makeWin(seedT, now));
  const [day, setDay] = useState(() => startOfLocalDay(seedT));
  const [playing, setPlaying] = useState(true);
  const [speed, setSpeedState] = useState(1);
  const [poster, setPoster] = useState<RecPoster | null>(null);
  const posterKey = useRef(0);
  const [store] = useState(() => new PositionStore(seedT));
  const [wasActive, setWasActive] = useState(active);
  if (active !== wasActive) {
    // Entering REC (button, or history navigation to ?mode=rec) opens a fresh window at the URL/entry time.
    setWasActive(active);
    if (active) {
      setWin(makeWin(seedT, now));
      setDay(startOfLocalDay(seedT));
      setPlaying(true);
    }
  }
  useEffect(() => {
    if (active) store.set(win.start + win.offset);
  }, [active, store, win]);

  const players = useRef(new Map<string, HlsPlayerHandle | null>());
  const latest = useRef({ win, playing, speed, day });
  useEffect(() => {
    latest.current = { win, playing, speed, day };
  });
  const each = useCallback((fn: (video: HTMLVideoElement) => void) => {
    players.current.forEach((handle) => {
      if (handle?.video) fn(handle.video);
    });
  }, []);

  /** bindPlayer returns the ref callback that registers a tile's HlsPlayer for transport and sync. */
  const bindPlayer = useCallback(
    (cameraId: string) => (handle: HlsPlayerHandle | null) => {
      if (handle) players.current.set(cameraId, handle);
      else players.current.delete(cameraId);
    },
    [],
  );

  const seek = useCallback(
    (t: number, options?: { play?: boolean; poster?: Omit<RecPoster, "key"> }) => {
      const cur = latest.current;
      const play = options?.play ?? true;
      store.set(t);
      setDay(startOfLocalDay(t));
      setPlaying(play);
      setPoster(options?.poster ? { ...options.poster, key: ++posterKey.current } : null);
      if (t >= cur.win.start + 1 && t <= cur.win.end - WINDOW_MARGIN_S) {
        each((v) => {
          v.currentTime = t - cur.win.start;
          if (play) void v.play().catch(() => {});
          else v.pause();
        });
      } else {
        setWin(makeWin(t, unixNow()));
      }
    },
    [each, store],
  );

  const play = useCallback(() => {
    setPlaying(true);
    each((v) => void v.play().catch(() => {}));
  }, [each]);
  const pause = useCallback(() => {
    setPlaying(false);
    each((v) => v.pause());
  }, [each]);
  const setSpeed = useCallback(
    (rate: number) => {
      setSpeedState(rate);
      each((v) => {
        v.playbackRate = rate;
      });
    },
    [each],
  );

  /** onMasterTime receives the master tile's clock (unix seconds) and rolls the window over near its end. */
  const onMasterTime = useCallback(
    (unix: number) => {
      store.set(unix);
      const cur = latest.current;
      if (unix >= cur.day + 24 * 3600) setDay(startOfLocalDay(unix));
      if (unix >= cur.win.end - ROLLOVER_MARGIN_S && cur.win.end < unixNow() - WINDOW_MARGIN_S) seek(unix, { play: cur.playing });
    },
    [seek, store],
  );

  return { win, day, poster, playing, speed, players, bindPlayer, seek, play, pause, setSpeed, onMasterTime, getPosition: store.get, subscribePosition: store.subscribe };
}
