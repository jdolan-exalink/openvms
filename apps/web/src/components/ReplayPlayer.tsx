import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from "react";
import { ZoomFrame } from "@/components/DigitalZoom";
import type { HlsPlayerHandle } from "@/components/HlsPlayer";
import { describeStreamError } from "@/lib/live/streamErrors";
import { cn } from "@/lib/cn";

/** Codecs offered to the gateway, in go2rtc's own list; only those this browser can play are sent. */
const CODEC_CANDIDATES = ["avc1.640029", "avc1.64002A", "avc1.640033", "hvc1.1.6.L153.B0", "mp4a.40.2", "mp4a.40.5", "flac", "opus"];

/** Seconds of already played video kept in the buffer; older ones are dropped so a long replay does not grow it. */
const KEEP_BEHIND_S = 30;
const TRIM_AFTER_S = 60;

function supportedCodecs(): string {
  if (typeof MediaSource === "undefined") return "";
  return CODEC_CANDIDATES.filter((c) => MediaSource.isTypeSupported(`video/mp4; codecs="${c}"`)).join(",");
}

type Status = "connecting" | "playing" | "ended" | "error";

export type ReplayPlayerProps = {
  cameraId: string;
  start: number;
  end: number;
  startOffset?: number;
  className?: string;
  onTime?: (unix: number) => void;
  ariaLabel?: string;
  controls?: boolean;
  muted?: boolean;
  autoPlay?: boolean;
  /** Which of the recorder's streams to replay: the grid uses the light one, a single camera the main one. */
  quality?: "sub" | "main";
  objectFit?: "contain" | "cover" | "fill";
  // Accepted so the player can replace HlsPlayer where it is mounted; a recorder replay has no use for them.
  rate?: number;
  poster?: string;
  posterFallback?: string;
  posterKey?: string | number;
  posterChip?: boolean;
};

/**
 * ReplayPlayer plays the recording of a Provision-ISR recorder camera: the gateway asks the recorder to
 * replay from the chosen instant (/media/v1/cameras/{id}/playback) and relays go2rtc's MSE websocket, the
 * same protocol as the live view. The recorder streams at normal speed and cannot seek, so a new instant
 * means a new session, and the handle exposes no video element: the multi-camera clock and speed controls,
 * which seek and change the rate of the elements they find, must leave this player alone.
 */
export const ReplayPlayer = forwardRef<HlsPlayerHandle, ReplayPlayerProps>(function ReplayPlayer(
  { cameraId, start, end, startOffset = 0, className, onTime, ariaLabel, controls = true, muted = true, autoPlay = true, quality = "main", objectFit = "contain" },
  ref,
) {
  const video = useRef<HTMLVideoElement>(null);
  const [status, setStatus] = useState<Status>("connecting");
  const [message, setMessage] = useState("");
  useImperativeHandle(ref, () => ({ video: null }), []);
  const from = Math.floor(start + startOffset);
  const to = Math.ceil(end);
  const options = useRef({ autoPlay });
  useEffect(() => {
    options.current = { autoPlay };
  });

  useEffect(() => {
    const el = video.current;
    if (!el) return;
    setStatus("connecting");
    setMessage("");
    const codecs = supportedCodecs();
    if (!codecs) {
      setStatus("error");
      setMessage("El navegador no soporta Media Source Extensions.");
      return;
    }
    const mediaSource = new MediaSource();
    const objectUrl = URL.createObjectURL(mediaSource);
    el.src = objectUrl;
    let sourceBuffer: SourceBuffer | undefined;
    let ws: WebSocket | undefined;
    let gotMedia = false;
    let failed = false;
    const queue: Uint8Array[] = [];

    const pump = () => {
      if (!sourceBuffer || sourceBuffer.updating || mediaSource.readyState !== "open") return;
      if (queue.length === 0) {
        const buffered = sourceBuffer.buffered;
        if (buffered.length > 0 && el.currentTime - buffered.start(0) > TRIM_AFTER_S) {
          try {
            sourceBuffer.remove(buffered.start(0), el.currentTime - KEEP_BEHIND_S);
          } catch {
            // The buffer was detached while closing.
          }
        }
        return;
      }
      try {
        sourceBuffer.appendBuffer(queue.shift() as BufferSource);
      } catch {
        fail("No se pudo reproducir la grabación.");
      }
    };
    function fail(text: string) {
      if (failed) return;
      failed = true;
      setStatus("error");
      setMessage(text);
      ws?.close();
    }

    const onSourceOpen = () => {
      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      const url = `${proto}//${location.host}/media/v1/cameras/${cameraId}/playback?start=${from}&end=${to}&quality=${quality}`;
      ws = new WebSocket(url);
      ws.binaryType = "arraybuffer";
      ws.onopen = () => ws?.send(JSON.stringify({ type: "mse", value: codecs }));
      ws.onmessage = (ev) => {
        if (typeof ev.data === "string") {
          let frame: { type?: string; value?: unknown; code?: unknown };
          try {
            frame = JSON.parse(ev.data);
          } catch {
            return;
          }
          if (frame.type === "error") {
            fail(describeStreamError(frame).label);
          } else if (frame.type === "mse" && typeof frame.value === "string" && !sourceBuffer) {
            try {
              sourceBuffer = mediaSource.addSourceBuffer(frame.value);
              sourceBuffer.mode = "segments";
              sourceBuffer.addEventListener("updateend", pump);
            } catch {
              fail("No se pudo reproducir la grabación.");
            }
          }
          return;
        }
        gotMedia = true;
        queue.push(new Uint8Array(ev.data as ArrayBuffer));
        pump();
        if (options.current.autoPlay) void el.play().catch(() => {});
      };
      ws.onclose = () => {
        if (failed) return;
        if (gotMedia) setStatus("ended");
        else fail("No hay grabación en ese tramo.");
      };
    };
    mediaSource.addEventListener("sourceopen", onSourceOpen, { once: true });
    const onPlaying = () => setStatus((s) => (s === "connecting" ? "playing" : s));
    // The recorder streams at normal speed: a faster rate would only starve the buffer.
    const onRate = () => {
      if (el.playbackRate !== 1) el.playbackRate = 1;
    };
    el.addEventListener("playing", onPlaying);
    el.addEventListener("ratechange", onRate);
    return () => {
      failed = true; // no message for a close this effect caused
      el.removeEventListener("playing", onPlaying);
      el.removeEventListener("ratechange", onRate);
      if (ws) {
        ws.onmessage = null;
        ws.onclose = null;
        ws.close();
      }
      el.removeAttribute("src");
      el.load();
      URL.revokeObjectURL(objectUrl);
    };
  }, [cameraId, from, to, quality]);

  return (
    <ZoomFrame resetKey={`${cameraId}:${from}:${to}`} className={className} picture={(zoom) => (
      <video
        ref={video}
        className={cn("size-full", objectFit === "fill" ? "object-fill" : objectFit === "cover" ? "object-cover" : "object-contain")}
        controls={controls && zoom.scale <= 1}
        muted={muted}
        playsInline
        aria-label={ariaLabel}
        onTimeUpdate={(e) => onTime?.(from + e.currentTarget.currentTime)}
      />
    )}>
      {status === "connecting" && (
        <span role="status" className="pointer-events-none absolute bottom-2 left-2 z-[3] flex items-center gap-1.5 rounded bg-black/60 px-2 py-0.5 text-[11px] text-white/90">
          <span aria-hidden className="size-2.5 animate-spin rounded-full border border-white/40 border-t-white motion-reduce:animate-none" />
          Cargando grabación…
        </span>
      )}
      {status === "ended" && (
        <span role="status" className="pointer-events-none absolute bottom-2 left-2 z-[3] rounded bg-black/60 px-2 py-0.5 text-[11px] text-white/90">
          Fin de la grabación de este tramo
        </span>
      )}
      {status === "error" && <div role="alert" className="absolute inset-0 z-[3] flex items-center justify-center p-4 text-center text-sm text-white/80">{message}</div>}
    </ZoomFrame>
  );
});
