import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/cn";

// Codecs offered to go2rtc, most preferred first (same list go2rtc's own player uses).
const CODECS = [
  "avc1.640029",
  "avc1.64002A",
  "avc1.640033",
  "hvc1.1.6.L153.B0",
  "mp4a.40.2",
  "mp4a.40.5",
  "flac",
  "opus",
];

function supportedCodecs(): string {
  const MS = window.MediaSource ?? (window as unknown as { ManagedMediaSource?: typeof MediaSource }).ManagedMediaSource;
  if (!MS) return "";
  return CODECS.filter((c) => MS.isTypeSupported(`video/mp4; codecs="${c}"`)).join();
}

type State = "connecting" | "playing" | "error";

/**
 * MsePlayer plays a camera live through the VMS media gateway, which relays Frigate's
 * go2rtc MSE websocket. Reconnects with backoff when the stream drops.
 */
export function MsePlayer({
  cameraId,
  quality = "sub",
  className,
  muted = true,
  onError,
}: {
  cameraId: string;
  quality?: "sub" | "main";
  className?: string;
  muted?: boolean;
  onError?: (msg: string) => void;
}) {
  const video = useRef<HTMLVideoElement>(null);
  const [state, setState] = useState<State>("connecting");
  const [message, setMessage] = useState("");

  useEffect(() => {
    const el = video.current;
    if (!el) return;
    let ws: WebSocket | null = null;
    let closed = false;
    let retry = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let objectURL = "";

    const connect = () => {
      if (closed) return;
      setState("connecting");
      const MS = window.MediaSource ?? (window as unknown as { ManagedMediaSource?: typeof MediaSource }).ManagedMediaSource;
      if (!MS) {
        setState("error");
        setMessage("El navegador no soporta Media Source Extensions.");
        return;
      }
      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      ws = new WebSocket(`${proto}//${location.host}/media/v1/cameras/${cameraId}/live?quality=${quality}`);
      ws.binaryType = "arraybuffer";
      let ms: MediaSource | null = null;
      let sb: SourceBuffer | null = null;
      const queue: ArrayBuffer[] = [];

      const pump = () => {
        if (!sb || sb.updating || queue.length === 0) return;
        try {
          sb.appendBuffer(queue.shift()!);
        } catch {
          // QuotaExceeded: drop the backlog, trimming below frees space
          queue.length = 0;
        }
      };

      ws.onopen = () => ws?.send(JSON.stringify({ type: "mse", value: supportedCodecs() }));
      ws.onmessage = (ev) => {
        if (typeof ev.data === "string") {
          const msg = JSON.parse(ev.data) as { type: string; value: string };
          if (msg.type === "mse") {
            ms = new MS();
            ms.addEventListener(
              "sourceopen",
              () => {
                if (!ms) return;
                URL.revokeObjectURL(objectURL);
                try {
                  sb = ms.addSourceBuffer(msg.value);
                } catch {
                  setState("error");
                  setMessage("Códec no soportado por el navegador.");
                  return;
                }
                sb.mode = "segments";
                sb.addEventListener("updateend", () => {
                  if (!sb || sb.updating) return;
                  // Keep ~10 s of buffer and stay close to the live edge.
                  const b = sb.buffered;
                  if (b.length > 0) {
                    const end = b.end(b.length - 1);
                    const start = b.start(0);
                    if (end - start > 15 && !sb.updating) {
                      try {
                        sb.remove(start, end - 10);
                        return;
                      } catch {
                        // ignore; next update trims
                      }
                    }
                    if (el.currentTime < end - 3) el.currentTime = end - 0.5;
                  }
                  pump();
                });
                pump();
              },
              { once: true },
            );
            objectURL = URL.createObjectURL(ms);
            el.src = objectURL;
            void el.play().catch(() => {});
          } else if (msg.type === "error") {
            setState("error");
            setMessage(msg.value);
            onError?.(msg.value);
          }
          return;
        }
        queue.push(ev.data as ArrayBuffer);
        if (queue.length > 60) queue.splice(0, queue.length - 60);
        pump();
        setState("playing");
        retry = 0;
      };
      ws.onclose = () => {
        if (closed) return;
        if (retry >= 2) setMessage("Sin conexión con la cámara, reintentando…");
        setState("connecting");
        retry = Math.min(retry + 1, 6);
        timer = setTimeout(connect, 500 * 2 ** retry);
      };
    };

    connect();
    const onVisibility = () => {
      if (document.visibilityState === "visible" && ws?.readyState !== WebSocket.OPEN) {
        clearTimeout(timer);
        connect();
      }
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      closed = true;
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisibility);
      ws?.close();
      el.removeAttribute("src");
      el.load();
      URL.revokeObjectURL(objectURL);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cameraId, quality]);

  return (
    <div className={cn("relative overflow-hidden bg-black", className)}>
      <video ref={video} className="size-full object-contain" autoPlay playsInline muted={muted} />
      {state !== "playing" && (
        <div className="absolute inset-0 flex items-center justify-center p-2 text-center text-xs text-white/70">
          {state === "error" ? message || "Error de video" : message || "Conectando…"}
        </div>
      )}
    </div>
  );
}
