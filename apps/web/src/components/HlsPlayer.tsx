import Hls from "hls.js";
import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from "react";
import { cn } from "@/lib/cn";

export type HlsPlayerHandle = { video: HTMLVideoElement | null };

const FRAME_WAIT_TIMEOUT_MS = 3000;

/**
 * onDecodedFrame calls back once the video shows a real frame at or after `minTime`
 * (requestVideoFrameCallback when available, else seeked/playing with data). Returns a cancel.
 */
function onDecodedFrame(el: HTMLVideoElement, minTime: number, done: () => void, timeoutMs?: number): () => void {
  let live = true;
  const finish = () => {
    if (!live) return;
    live = false;
    cleanup();
    done();
  };
  const ready = () => el.readyState >= 2 && el.currentTime >= minTime - 1;
  const check = () => {
    if (live && ready()) finish();
  };
  let rvfc: number | undefined;
  const arm = () => {
    rvfc = el.requestVideoFrameCallback?.(() => {
      if (!live) return;
      if (ready()) finish();
      else arm();
    });
  };
  if (typeof el.requestVideoFrameCallback === "function") arm();
  else {
    el.addEventListener("seeked", check);
    el.addEventListener("playing", check);
    el.addEventListener("canplay", check);
  }
  const timer = timeoutMs ? window.setTimeout(finish, timeoutMs) : undefined;
  function cleanup() {
    if (rvfc !== undefined) el.cancelVideoFrameCallback?.(rvfc);
    el.removeEventListener("seeked", check);
    el.removeEventListener("playing", check);
    el.removeEventListener("canplay", check);
    if (timer) window.clearTimeout(timer);
  }
  return () => {
    live = false;
    cleanup();
  };
}

/**
 * HlsPlayer plays a recording range from the origin Frigate through the media gateway
 * (/media/v1/cameras/{id}/vod/{start}/{end}/master.m3u8). startOffset seeks inside the
 * range once the playlist is loaded.
 */
export const HlsPlayer = forwardRef<
  HlsPlayerHandle,
  {
    cameraId: string;
    start: number;
    end: number;
    startOffset?: number;
    className?: string;
    onTime?: (unix: number) => void;
    ariaLabel?: string;
    /** Native transport controls (default true); the Live REC grid drives playback itself. */
    controls?: boolean;
    muted?: boolean;
    /** Start playing once the playlist is loaded (default true). Read at load time only. */
    autoPlay?: boolean;
    /** Playback speed applied once the playlist is loaded (default: leave the browser's). */
    rate?: number;
    /**
     * Image shown over the video until it has a decoded frame at the target time (then cross-faded).
     * `posterKey` re-arms it for an in-window seek; `posterFallback` is tried if `poster` fails to load.
     */
    poster?: string;
    posterFallback?: string;
    posterKey?: string | number;
    /** Show the "Cargando grabación…" chip while the poster is up. */
    posterChip?: boolean;
  }
>(function HlsPlayer({ cameraId, start, end, startOffset = 0, className, onTime, ariaLabel, controls = true, muted, autoPlay = true, rate, poster, posterFallback, posterKey, posterChip }, ref) {
  const video = useRef<HTMLVideoElement>(null);
  const [error, setError] = useState("");
  const [waiting, setWaiting] = useState(true);
  const [posterSrc, setPosterSrc] = useState(poster);
  const [prevPoster, setPrevPoster] = useState(poster);
  if (poster !== prevPoster) {
    setPrevPoster(poster);
    if (poster) setPosterSrc(poster);
  }
  useImperativeHandle(ref, () => ({ video: video.current }), []);
  // Latest values for the load-time callback below, without reloading the stream when they change.
  const loadOptions = useRef({ autoPlay, rate });
  useEffect(() => {
    loadOptions.current = { autoPlay, rate };
  });

  useEffect(() => {
    const el = video.current;
    if (!el) return;
    setError("");
    setWaiting(true);
    const cancelWait = onDecodedFrame(el, startOffset, () => setWaiting(false));
    const src = `/media/v1/cameras/${cameraId}/vod/${Math.floor(start)}/${Math.ceil(end)}/master.m3u8`;
    const seek = () => {
      if (startOffset > 0) el.currentTime = startOffset;
      if (loadOptions.current.rate) el.playbackRate = loadOptions.current.rate;
      if (loadOptions.current.autoPlay) void el.play().catch(() => {});
    };
    if (Hls.isSupported()) {
      // No worker: the Content-Security-Policy does not allow blob: workers.
      const hls = new Hls({
        enableWorker: false,
        xhrSetup: (xhr) => {
          xhr.withCredentials = true;
        },
      });
      hls.on(Hls.Events.MANIFEST_PARSED, seek);
      hls.on(Hls.Events.ERROR, (_, data) => {
        if (!data.fatal) return;
        if (data.response?.code === 404) setError("No hay grabación en ese rango.");
        else if (data.response?.code === 403) setError("No tenés permiso para ver grabaciones de esta cámara.");
        else setError("No se pudo reproducir la grabación.");
        hls.destroy();
      });
      hls.loadSource(src);
      hls.attachMedia(el);
      return () => {
        cancelWait();
        hls.destroy();
      };
    }
    if (el.canPlayType("application/vnd.apple.mpegurl")) {
      el.src = src; // Safari plays HLS natively
      el.addEventListener("loadedmetadata", seek, { once: true });
      return () => {
        cancelWait();
        el.removeAttribute("src");
        el.load();
      };
    }
    setError("El navegador no puede reproducir HLS.");
    return cancelWait;
  }, [cameraId, start, end, startOffset]);

  // A poster set for a seek inside the loaded window (no remount): show it until the new frame is decoded.
  const firstPoster = useRef(true);
  useEffect(() => {
    if (firstPoster.current) {
      firstPoster.current = false;
      return;
    }
    const el = video.current;
    if (!el || posterKey === undefined || !poster) return;
    setWaiting(true);
    return onDecodedFrame(el, startOffset, () => setWaiting(false), FRAME_WAIT_TIMEOUT_MS);
  }, [posterKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const showPoster = !!posterSrc && waiting;

  return (
    <div className={cn("relative bg-black", className)}>
      <video
        ref={video}
        className="size-full object-contain"
        controls={controls}
        muted={muted}
        playsInline
        aria-label={ariaLabel}
        onTimeUpdate={(e) => onTime?.(start + e.currentTarget.currentTime)}
      />
      {posterSrc && (
        <img
          src={posterSrc}
          alt=""
          draggable={false}
          onError={() => posterFallback && posterSrc !== posterFallback && setPosterSrc(posterFallback)}
          className={cn("pointer-events-none absolute inset-0 size-full object-contain transition-opacity duration-150 motion-reduce:transition-none", showPoster ? "opacity-100" : "opacity-0")}
        />
      )}
      {showPoster && posterChip && !error && (
        <span role="status" className="pointer-events-none absolute bottom-2 left-2 flex items-center gap-1.5 rounded bg-black/60 px-2 py-0.5 text-[11px] text-white/90">
          <span aria-hidden className="size-2.5 animate-spin rounded-full border border-white/40 border-t-white motion-reduce:animate-none" />
          Cargando grabación…
        </span>
      )}
      {error && <div className="absolute inset-0 flex items-center justify-center p-4 text-center text-sm text-white/80">{error}</div>}
    </div>
  );
});
