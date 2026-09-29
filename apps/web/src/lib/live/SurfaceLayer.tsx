import { createContext, useContext, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import type { PlayerSession } from "./PlayerSession";
import { VideoSurfaceLayerController } from "./surfaceLayer";

const LayerContext = createContext<VideoSurfaceLayerController | null>(null);

/**
 * VideoSurfaceLayer is mounted once in the authenticated shell, next to the
 * PlayerSessionProvider. It renders the fixed overlay that hosts the persistent `<video>`
 * elements; grid cells render a SurfaceSlot and the layer positions the video over it.
 * The layer sits above the page (z-index 1, pointer-events none) so clicks reach the tile
 * underneath, and tile controls use a higher z-index to stay above the video.
 */
export function VideoSurfaceLayer({ children }: { children: ReactNode }) {
  const [controller] = useState(() => new VideoSurfaceLayerController());
  const host = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    controller.setHost(host.current);
    return () => controller.setHost(null);
  }, [controller]);
  useEffect(() => {
    controller.start();
    return () => controller.stop();
  }, [controller]);
  return (
    <LayerContext.Provider value={controller}>
      {children}
      <div ref={host} data-testid="video-surface-layer" aria-hidden="true" className="pointer-events-none fixed inset-0 z-[1] overflow-hidden" />
    </LayerContext.Provider>
  );
}

/** useSurfaceLayer returns the layer controller, or null outside a VideoSurfaceLayer. */
export function useSurfaceLayer(): VideoSurfaceLayerController | null {
  return useContext(LayerContext);
}

/**
 * SurfaceSlot is the placeholder a grid cell renders for a session. It registers its rect with
 * the layer, which overlays the session's persistent `<video>` on it; unmounting only hides
 * the video, never moves or stops it. Give it the size (usually `absolute inset-0`).
 */
export function SurfaceSlot({ session, className }: { session: PlayerSession | null; className?: string }) {
  const layer = useSurfaceLayer();
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!layer || !session || !el) return;
    return layer.register(session, el);
  }, [layer, session]);
  return <div ref={ref} data-surface-slot={session?.cameraId} className={className} />;
}
