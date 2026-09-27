import Hls from "hls.js";
import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from "react";
import { cn } from "@/lib/cn";

export type HlsPlayerHandle = { video: HTMLVideoElement | null };

/**
 * HlsPlayer plays a recording range from the origin Frigate through the media gateway
 * (/media/v1/cameras/{id}/vod/{start}/{end}/master.m3u8). startOffset seeks inside the
 * range once the playlist is loaded.
 */
export const HlsPlayer = forwardRef<
  HlsPlayerHandle,
  { cameraId: string; start: number; end: number; startOffset?: number; className?: string; onTime?: (unix: number) => void }
>(function HlsPlayer({ cameraId, start, end, startOffset = 0, className, onTime }, ref) {
  const video = useRef<HTMLVideoElement>(null);
  const [error, setError] = useState("");
  useImperativeHandle(ref, () => ({ video: video.current }), []);

  useEffect(() => {
    const el = video.current;
    if (!el) return;
    setError("");
    const src = `/media/v1/cameras/${cameraId}/vod/${Math.floor(start)}/${Math.ceil(end)}/master.m3u8`;
    const seek = () => {
      if (startOffset > 0) el.currentTime = startOffset;
      void el.play().catch(() => {});
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
      return () => hls.destroy();
    }
    if (el.canPlayType("application/vnd.apple.mpegurl")) {
      el.src = src; // Safari plays HLS natively
      el.addEventListener("loadedmetadata", seek, { once: true });
      return () => {
        el.removeAttribute("src");
        el.load();
      };
    }
    setError("El navegador no puede reproducir HLS.");
  }, [cameraId, start, end, startOffset]);

  return (
    <div className={cn("relative bg-black", className)}>
      <video
        ref={video}
        className="size-full object-contain"
        controls
        playsInline
        onTimeUpdate={(e) => onTime?.(start + e.currentTarget.currentTime)}
      />
      {error && <div className="absolute inset-0 flex items-center justify-center p-4 text-center text-sm text-white/80">{error}</div>}
    </div>
  );
});
