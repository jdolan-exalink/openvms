import { useCallback, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { PlayerSession, type SessionSnapshot } from "@/lib/live/PlayerSession";
import { usePlayerSession } from "@/lib/live/PlayerSessionProvider";
import { SurfaceSlot, useSurfaceLayer } from "@/lib/live/SurfaceLayer";
import { IDENTITY_ZOOM, type DigitalZoom } from "@/lib/live/digitalZoom";
import { ZoomFrame } from "./DigitalZoom";
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
  /** How the picture fills the tile. Map windows use cover so the snapshot underneath is fully hidden. */
  objectFit?: "contain" | "cover";
};

/**
 * MsePlayer plays a camera live through the VMS media gateway, which relays Frigate's
 * go2rtc MSE websocket. It is a thin view over a PlayerSession (lib/live), which owns the
 * `<video>`, the websocket and the reconnect policy; this component only renders it.
 */
export function MsePlayer({ persistent = false, surface = false, objectFit, ...props }: MsePlayerProps) {
  const layer = useSurfaceLayer();
  if (persistent && surface && layer) return <SurfaceMsePlayer {...props} />;
  return persistent ? <PersistentMsePlayer {...props} objectFit={objectFit} /> : <PrivateMsePlayer {...props} objectFit={objectFit} />;
}

function PlayerChrome({
  snapshot,
  cameraId,
  onRetry,
}: {
  snapshot: SessionSnapshot;
  cameraId?: string;
  onRetry?: () => void;
}) {
  const showing = snapshot.state === "ACTIVE" || snapshot.state === "WARM";
  if (showing) return null;
  if (cameraId) return <PlayerStatusOverlay cameraId={cameraId} snapshot={snapshot} onRetry={onRetry} />;
  return (
    <div className="absolute inset-0 flex items-center justify-center p-2 text-center text-xs text-white/70">
      {snapshot.state === "ERROR" ? snapshot.message || "Error de video" : snapshot.message || "Conectando…"}
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
    return () => session?.setSuspended("offscreen", false);
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
        // A box that has not been laid out yet is not "off screen"; suspending it
        // drops the socket and the next paint is a black tile.
        if (!last || last.boundingClientRect.width < 1 || last.boundingClientRect.height < 1) return;
        setInViewport(last.isIntersecting);
      },
      { rootMargin: "120px" },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [ref]);
  return inViewport;
}

/** PersistentMsePlayer shows a manager-owned session; unmounting only detaches the `<video>`. */
function PersistentMsePlayer({ cameraId, quality = "sub", serverId, active, suspended, className, muted = true, onError, objectFit }: Omit<MsePlayerProps, "persistent" | "surface">) {
  const frame = useRef<HTMLDivElement>(null);
  const stage = useRef<HTMLDivElement>(null);
  const layer = useSurfaceLayer();
  const { session, snapshot } = usePersistentSession({ cameraId, quality, serverId, active, suspended, onError }, frame);
  useLayoutEffect(() => {
    const el = stage.current;
    if (!session || !el) return;
    session.setMuted(muted);
    session.attach(el);
    session.setObjectFit(objectFit);
    return () => {
      if (!layer?.restore(session)) session.detach();
    };
  }, [session, muted, layer, objectFit]);
  return (
    <ZoomFrame frameRef={frame} stageRef={stage} resetKey={`${cameraId}:${quality}`} className={className}>
      <PlayerChrome snapshot={snapshot} cameraId={cameraId} onRetry={() => session?.retryNow()} />
    </ZoomFrame>
  );
}

/** SurfaceMsePlayer leaves the `<video>` in the VideoSurfaceLayer and only reserves its slot here. */
function SurfaceMsePlayer({ cameraId, quality = "sub", serverId, active, suspended, className, muted = true, onError }: Omit<MsePlayerProps, "persistent" | "surface">) {
  const frame = useRef<HTMLDivElement>(null);
  const layer = useSurfaceLayer();
  const { session, snapshot } = usePersistentSession({ cameraId, quality, serverId, active, suspended, onError }, frame);
  useEffect(() => {
    session?.setMuted(muted);
  }, [session, muted]);
  const onZoom = useCallback(
    (zoom: DigitalZoom) => {
      if (session) layer?.setPictureZoom(session, zoom);
    },
    [session, layer],
  );
  useEffect(() => {
    if (!session || !layer) return;
    return () => layer.setPictureZoom(session, IDENTITY_ZOOM);
  }, [session, layer]);
  const showing = snapshot.state === "ACTIVE" || snapshot.state === "WARM";
  return (
    <ZoomFrame frameRef={frame} resetKey={`${cameraId}:${quality}`} className={className} scalePicture={false} onZoom={onZoom}>
      <SurfaceSlot session={session} className="absolute inset-0" />
      {!showing && <PlayerStatusOverlay cameraId={cameraId} snapshot={snapshot} onRetry={() => session?.retryNow()} />}
    </ZoomFrame>
  );
}

/** PrivateMsePlayer owns its session: it closes with the component (the legacy behaviour). */
function PrivateMsePlayer({ cameraId, quality = "sub", className, muted = true, onError, objectFit }: Omit<MsePlayerProps, "persistent" | "surface">) {
  const stage = useRef<HTMLDivElement>(null);
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
    const host = stage.current;
    if (!host) return;
    const s = new PlayerSession({ cameraId, quality, muted: mutedRef.current, onError: (m) => onErrorRef.current?.(m) });
    session.current = s;
    setSnapshot(s.getSnapshot());
    const off = s.subscribe(() => setSnapshot(s.getSnapshot()));
    s.attach(host);
    s.setObjectFit(objectFit);
    s.connect();
    return () => {
      off();
      s.close("unmount");
      session.current = null;
    };
  }, [cameraId, quality, objectFit]);

  return (
    <ZoomFrame stageRef={stage} resetKey={`${cameraId}:${quality}`} className={className}>
      <PlayerChrome snapshot={snapshot} />
    </ZoomFrame>
  );
}
