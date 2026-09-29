import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/cn";
import { PlayerSession, type SessionSnapshot } from "@/lib/live/PlayerSession";

/**
 * MsePlayer plays a camera live through the VMS media gateway, which relays Frigate's
 * go2rtc MSE websocket. It is a thin view over a PlayerSession (lib/live), which owns the
 * `<video>`, the websocket and the reconnect policy; this component only renders it.
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
  const container = useRef<HTMLDivElement>(null);
  const session = useRef<PlayerSession | null>(null);
  const [snapshot, setSnapshot] = useState<SessionSnapshot>({ state: "UNINITIALIZED", message: "" });
  const onErrorRef = useRef(onError);
  const mutedRef = useRef(muted);
  useEffect(() => {
    onErrorRef.current = onError;
    mutedRef.current = muted;
    session.current?.setMuted(muted);
  });

  useEffect(() => {
    const host = container.current;
    if (!host) return;
    const s = new PlayerSession({ cameraId, quality, muted: mutedRef.current, onError: (m) => onErrorRef.current?.(m) });
    session.current = s;
    setSnapshot(s.getSnapshot());
    const off = s.subscribe(() => setSnapshot(s.getSnapshot()));
    s.attach(host);
    s.connect();
    return () => {
      off();
      s.close("unmount");
      session.current = null;
    };
  }, [cameraId, quality]);

  const showing = snapshot.state === "ACTIVE" || snapshot.state === "WARM";
  return (
    <div ref={container} className={cn("relative overflow-hidden bg-black", className)}>
      {!showing && (
        <div className="absolute inset-0 flex items-center justify-center p-2 text-center text-xs text-white/70">
          {snapshot.state === "ERROR" ? snapshot.message || "Error de video" : snapshot.message || "Conectando…"}
        </div>
      )}
    </div>
  );
}
