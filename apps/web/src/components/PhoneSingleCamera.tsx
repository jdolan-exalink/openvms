import { useEffect, useLayoutEffect, useRef } from "react";
import type { Schemas } from "@/api/client";
import { ZoomFrame } from "@/components/DigitalZoom";
import { MsePlayer } from "@/components/MsePlayer";
import { cn } from "@/lib/cn";
import type { DigitalZoom } from "@/lib/live/digitalZoom";
import { usePlayerSession } from "@/lib/live/PlayerSessionProvider";
import { snapshotUrl } from "@/lib/phoneSnapshot";

type Camera = Schemas["Camera"];

/** Scale a double tap zooms the single camera to. */
const DOUBLE_TAP_ZOOM = 2.5;

/** The single-camera picture box: 16:9 inline, the whole container when it fills its parent. */
export const stageClass = (fill: boolean) => cn("relative w-full overflow-hidden bg-video", fill ? "size-full" : "aspect-video");

/**
 * SingleCamera opens instantly: the sub session of the list (already playing, same persistent
 * session) is shown at once while the main session warms in a layer on top. A session's
 * `<video>` stays transparent until its decoder has a picture (PlayerSession.revealFrame), so
 * the sub picture shows through until main reveals its first frame and then main covers it.
 * The sub session stays mounted underneath so going back to the list is instant.
 * It is the one live player of the single view and of the focus layout.
 */
export function SingleCamera({ camera, fill, onZoom }: { camera: Camera; fill: boolean; onZoom?: (zoom: DigitalZoom) => void }) {
  const main = usePlayerSession(camera.id, "main", camera.server_id);
  const stage = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = stage.current;
    if (!main || !el) return;
    main.setMuted(true);
    main.attach(el);
    return () => main.detach();
  }, [main]);
  // Inline the picture fills a 16:9 box; when it fills its parent it is letterboxed (contain) on any screen shape,
  // for the main layer and for the sub picture under it alike.
  useEffect(() => {
    main?.setObjectFit(fill ? "contain" : undefined);
  }, [main, fill]);
  return (
    <div data-testid="phone-stage" className={stageClass(fill)}>
      {/* Pinch/pan/double tap live only here: the list needs one-finger swipes for paging. A new camera resets the zoom. */}
      <ZoomFrame
        touch
        enhanceable={false}
        doubleTapZoom={DOUBLE_TAP_ZOOM}
        resetKey={camera.id}
        onZoom={onZoom}
        className="absolute inset-0 size-full"
        picture={() => (
          <>
            <img src={snapshotUrl(camera.id)} alt="" draggable={false} className="absolute inset-0 size-full object-contain" />
            <MsePlayer persistent cameraId={camera.id} quality="sub" serverId={camera.server_id} objectFit={fill ? "contain" : "fill"} className="absolute inset-0 size-full" />
            <div ref={stage} data-testid="phone-main-layer" className="absolute inset-0" />
          </>
        )}
      />
    </div>
  );
}
