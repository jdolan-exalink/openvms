import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { cn } from "@/lib/cn";
import { PlayerSession, type SessionSnapshot } from "@/lib/live/PlayerSession";
import { usePlayerSession } from "@/lib/live/PlayerSessionProvider";
import { SurfaceSlot, useSurfaceLayer } from "@/lib/live/SurfaceLayer";
import { PlayerStatusOverlay } from "./PlayerStatusOverlay";

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
  /**
   * With `persistent`, render the video in the app-wide VideoSurfaceLayer (feature flag
   * `videoSurfaceLayer`) instead of inside this element, so it is never re-parented by
   * layout, drag and drop or expand. Ignored outside a layer.
   */
  surface?: boolean;
  /** Frigate server of the camera; its sessions share reconnect backoff. */
  serverId?: string;
  /**
   * False while the tile is hidden on purpose (another tile is expanded): the session is kept
   * WARM and streaming so it resumes instantly. Default true.
   */
  active?: boolean;
  /**
   * Pause the transport while something else covers the tile (the Live REC mode): the session
   * stays alive with its last frame and reconnects when this turns false again. Persistent only.
   */
  suspended?: boolean;
};

/**
 * MsePlayer plays a camera live through the VMS media gateway, which relays Frigate's
 * go2rtc MSE websocket. It is a thin view over a PlayerSession (lib/live), which owns the
 * `<video>`, the websocket and the reconnect policy; this component only renders it.
 */
export function MsePlayer({ persistent = false, surface = false, ...props }: MsePlayerProps) {
  const layer = useSurfaceLayer();
  if (persistent && surface && layer) return <SurfaceMsePlayer {...props} />;
  return persistent ? <PersistentMsePlayer {...props} /> : <PrivateMsePlayer {...props} />;
}

function PlayerView({
  host,
  snapshot,
  className,
  cameraId,
  onRetry,
}: {
  host: React.RefObject<HTMLDivElement | null>;
  snapshot: SessionSnapshot;
  className?: string;
  /** Set for persistent sessions: shows the last frame / snapshot and retry instead of plain text. */
  cameraId?: string;
  onRetry?: () => void;
}) {
  const showing = snapshot.state === "ACTIVE" || snapshot.state === "WARM";
  return (
    <div ref={host} className={cn("relative overflow-hidden bg-black", className)}>
      {!showing && cameraId && <PlayerStatusOverlay cameraId={cameraId} snapshot={snapshot} onRetry={onRetry} />}
      {!showing && !cameraId && (
        <div className="absolute inset-0 flex items-center justify-center p-2 text-center text-xs text-white/70">
          {snapshot.state === "ERROR" ? snapshot.message || "Error de video" : snapshot.message || "Conectando…"}
        </div>
      )}
    </div>
  );
}

const IDLE_SNAPSHOT: SessionSnapshot = { state: "UNINITIALIZED", message: "", error: null, poster: null };
const noSubscribe = () => () => {};

/** usePersistentSession acquires the manager-owned session of a camera and mirrors its state. */
function usePersistentSession(
  { cameraId, quality = "sub", serverId, active = true, suspended = false, onError }: Pick<MsePlayerProps, "cameraId" | "quality" | "serverId" | "active" | "suspended" | "onError">,
  host: React.RefObject<HTMLElement | null>,
) {
  const session = usePlayerSession(cameraId, quality, serverId);
  const inViewport = useInViewport(host);
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
    if (!session) return;
    if (active) session.markActive("visible");
    else session.markWarm("hidden");
  }, [session, active]);
  // Tiles scrolled out of view stop their transport and keep the last frame; they resume on return.
  // A tile hidden by expand is not "offscreen": it stays WARM (above) instead of reconnecting later.
  useEffect(() => {
    session?.setSuspended("offscreen", active && !inViewport);
  }, [session, active, inViewport]);
  useEffect(() => {
    session?.setSuspended("rec", suspended);
  }, [session, suspended]);
  return { session, snapshot };
}

/** useInViewport reports whether the element intersects the viewport (true when unsupported). */
function useInViewport(ref: React.RefObject<HTMLElement | null>): boolean {
  const [inViewport, setInViewport] = useState(true);
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      (entries) => {
        const last = entries[entries.length - 1];
        if (last) setInViewport(last.isIntersecting);
      },
      { rootMargin: "120px" },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [ref]);
  return inViewport;
}

/** PersistentMsePlayer shows a manager-owned session; unmounting only detaches the `<video>`. */
function PersistentMsePlayer({ cameraId, quality = "sub", serverId, active, suspended, className, muted = true, onError }: Omit<MsePlayerProps, "persistent" | "surface">) {
  const host = useRef<HTMLDivElement>(null);
  const { session, snapshot } = usePersistentSession({ cameraId, quality, serverId, active, suspended, onError }, host);
  useEffect(() => {
    const el = host.current;
    if (!session || !el) return;
    session.setMuted(muted);
    session.attach(el);
    return () => session.detach();
  }, [session, muted]);
  return <PlayerView host={host} snapshot={snapshot} className={className} cameraId={cameraId} onRetry={() => session?.retryNow()} />;
}

/** SurfaceMsePlayer leaves the `<video>` in the VideoSurfaceLayer and only reserves its slot here. */
function SurfaceMsePlayer({ cameraId, quality = "sub", serverId, active, suspended, className, muted = true, onError }: Omit<MsePlayerProps, "persistent" | "surface">) {
  const host = useRef<HTMLDivElement>(null);
  const { session, snapshot } = usePersistentSession({ cameraId, quality, serverId, active, suspended, onError }, host);
  useEffect(() => {
    session?.setMuted(muted);
  }, [session, muted]);
  const showing = snapshot.state === "ACTIVE" || snapshot.state === "WARM";
  return (
    <div ref={host} className={cn("relative overflow-hidden bg-black", className)}>
      <SurfaceSlot session={session} className="absolute inset-0" />
      {!showing && <PlayerStatusOverlay cameraId={cameraId} snapshot={snapshot} onRetry={() => session?.retryNow()} />}
    </div>
  );
}

/** PrivateMsePlayer owns its session: it closes with the component (the legacy behaviour). */
function PrivateMsePlayer({ cameraId, quality = "sub", className, muted = true, onError }: Omit<MsePlayerProps, "persistent" | "surface">) {
  const container = useRef<HTMLDivElement>(null);
  const session = useRef<PlayerSession | null>(null);
  const [snapshot, setSnapshot] = useState<SessionSnapshot>(IDLE_SNAPSHOT);
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
