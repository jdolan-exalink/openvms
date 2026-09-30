import { useEffect, useRef } from "react";
import { decideSync, SYNC_INTERVAL_MS } from "@/lib/playbackSync";

type VideoGetter = (cameraId: string) => HTMLVideoElement | null | undefined;

/**
 * useSyncedPlayback wires the master video's transport (play, pause, seek, speed) to every
 * follower and runs a periodic drift-correction loop against the master's currentTime.
 * The loop is skipped while the tab is hidden or the master is not playing.
 */
export function useSyncedPlayback(masterId: string, cameraIds: string[], getVideo: VideoGetter) {
  const idsKey = cameraIds.join("|");
  const getRef = useRef(getVideo);
  useEffect(() => {
    getRef.current = getVideo;
  });

  useEffect(() => {
    if (!masterId) return;
    const followers = () =>
      idsKey
        .split("|")
        .filter((id) => id && id !== masterId)
        .map((id) => getRef.current(id))
        .filter((v): v is HTMLVideoElement => !!v);

    let bound: HTMLVideoElement | null = null;
    let unbind = () => {};
    const bind = (m: HTMLVideoElement) => {
      unbind();
      bound = m;
      const onPlay = () => followers().forEach((f) => void f.play().catch(() => {}));
      const onPause = () => followers().forEach((f) => f.pause());
      const onSeeked = () => followers().forEach((f) => (f.currentTime = m.currentTime));
      const onRate = () => followers().forEach((f) => (f.playbackRate = m.playbackRate));
      m.addEventListener("play", onPlay);
      m.addEventListener("pause", onPause);
      m.addEventListener("seeked", onSeeked);
      m.addEventListener("ratechange", onRate);
      unbind = () => {
        m.removeEventListener("play", onPlay);
        m.removeEventListener("pause", onPause);
        m.removeEventListener("seeked", onSeeked);
        m.removeEventListener("ratechange", onRate);
      };
    };

    const tick = () => {
      const m = getRef.current(masterId);
      if (!m) return;
      if (m !== bound) bind(m); // the player was remounted (new VOD window)
      if (document.hidden || m.paused || m.seeking || m.readyState < 3) return;
      const speed = m.playbackRate;
      for (const f of followers()) {
        const action = decideSync(m.currentTime, speed, {
          currentTime: f.currentTime,
          playbackRate: f.playbackRate,
          paused: f.paused,
          seeking: f.seeking,
          readyState: f.readyState,
          unavailable: !!f.error || (f.readyState === 0 && f.networkState === 3),
        });
        if (action.kind === "rate") f.playbackRate = action.rate;
        else if (action.kind === "seek") f.currentTime = action.time;
      }
    };
    tick();
    const id = setInterval(tick, SYNC_INTERVAL_MS);
    return () => {
      clearInterval(id);
      unbind();
      bound = null;
    };
  }, [masterId, idsKey]);
}
