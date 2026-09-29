import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { cn } from "@/lib/cn";
import { PlayerSession, type SessionSnapshot } from "@/lib/live/PlayerSession";
import { usePlayerSession } from "@/lib/live/PlayerSessionProvider";

type MsePlayerProps = {
  cameraId: string;
  quality?: "sub" | "main";
  className?: string;
  muted?: boolean;
  onError?: (msg: string) => void;
  /**
   * Use a session from the app-wide PlayerSessionManager (feature flag `persistentPlayers`),
   * so the stream survives remounts. Off by default: the player owns a private session.
   */
  persistent?: boolean;
};

/**
 * MsePlayer plays a camera live through the VMS media gateway, which relays Frigate's
 * go2rtc MSE websocket. It is a thin view over a PlayerSession (lib/live), which owns the
 * `<video>`, the websocket and the reconnect policy; this component only renders it.
 */
export function MsePlayer({ persistent = false, ...props }: MsePlayerProps) {
  return persistent ? <PersistentMsePlayer {...props} /> : <PrivateMsePlayer {...props} />;
}

function PlayerView({ host, snapshot, className }: { host: React.RefObject<HTMLDivElement | null>; snapshot: SessionSnapshot; className?: string }) {
  const showing = snapshot.state === "ACTIVE" || snapshot.state === "WARM";
  return (
    <div ref={host} className={cn("relative overflow-hidden bg-black", className)}>
      {!showing && (
        <div className="absolute inset-0 flex items-center justify-center p-2 text-center text-xs text-white/70">
          {snapshot.state === "ERROR" ? snapshot.message || "Error de video" : snapshot.message || "Conectando…"}
        </div>
      )}
    </div>
  );
}

const IDLE_SNAPSHOT: SessionSnapshot = { state: "UNINITIALIZED", message: "" };
const noSubscribe = () => () => {};

/** PersistentMsePlayer shows a manager-owned session; unmounting only detaches the `<video>`. */
function PersistentMsePlayer({ cameraId, quality = "sub", className, muted = true, onError }: Omit<MsePlayerProps, "persistent">) {
  const host = useRef<HTMLDivElement>(null);
  const session = usePlayerSession(cameraId, quality);
  const snapshot = useSyncExternalStore(session?.subscribe ?? noSubscribe, session?.getSnapshot ?? (() => IDLE_SNAPSHOT));
  const onErrorRef = useRef(onError);
  useEffect(() => {
    onErrorRef.current = onError;
  });
  useEffect(() => {
    if (!session) return;
    return session.onServerError((m) => onErrorRef.current?.(m));
  }, [session]);
  useEffect(() => {
    const el = host.current;
    if (!session || !el) return;
    session.setMuted(muted);
    session.attach(el);
    return () => session.detach();
  }, [session, muted]);
  return <PlayerView host={host} snapshot={snapshot} className={className} />;
}

/** PrivateMsePlayer owns its session: it closes with the component (the legacy behaviour). */
function PrivateMsePlayer({ cameraId, quality = "sub", className, muted = true, onError }: Omit<MsePlayerProps, "persistent">) {
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

  return <PlayerView host={container} snapshot={snapshot} className={className} />;
}
