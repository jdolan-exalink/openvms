import type { CameraEntity } from "@/lib/maps/types";
import type { HoverStage } from "@/lib/maps/hoverIntent";
import { MsePlayer } from "@/components/MsePlayer";
import { usePlayerSession } from "@/lib/live/PlayerSessionProvider";
import { computeDisplayState } from "@/lib/maps/entityIndex";
import { IconButton } from "@/components/ui";
import { STATE_DOT, STATE_TEXT } from "../stateTone";
import { ExternalLink, Pin, Video } from "lucide-react";

export interface CameraPreviewProps {
  camera: CameraEntity;
  siteName?: string;
  stage: HoverStage;
  position: { x: number; y: number };
  onPin?: (cameraId: string) => void;
  onOpenLive?: (cameraId: string) => void;
  liveOnHover?: boolean;
  /** Same shared session as Live. The picture stays inside this card. */
  persistent?: boolean;
  canPreview?: boolean;
  onHoverEnter?: () => void;
  onHoverLeave?: () => void;
}

export function CameraPreview({
  camera,
  siteName,
  stage,
  position,
  onPin,
  onOpenLive,
  liveOnHover = false,
  persistent = false,
  canPreview = true,
  onHoverEnter,
  onHoverLeave,
}: CameraPreviewProps) {
  const shouldRenderVideo = stage === "live" && liveOnHover && canPreview;
  const shouldPrewarm = canPreview && (stage === "prewarm" || stage === "live");
  // Opens the shared sub session before the picture is shown. The player below places the <video>.
  usePlayerSession(shouldPrewarm ? camera.id : "", "sub", camera.serverId);

  const displayState = computeDisplayState(
    camera.status,
    camera.activeAlarms,
    camera.metadata?.serverOffline as boolean | undefined,
  );
  const snapshotUrl = `/media/v1/cameras/${camera.id}/snapshot.jpg?h=240`;

  // Position popover safely relative to cursor (default above/right)
  const left = Math.min(position.x + 16, typeof window !== "undefined" ? window.innerWidth - 300 : 800);
  const top = Math.max(position.y - 120, 20);

  return (
    <div
      data-testid="camera-hover-preview"
      onMouseEnter={onHoverEnter}
      onMouseLeave={onHoverLeave}
      onFocus={onHoverEnter}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) onHoverLeave?.();
      }}
      style={{ left: `${left}px`, top: `${top}px` }}
      data-map-source={camera.id}
      data-map-source-rank="2"
      className="pointer-events-auto absolute w-72 rounded-m3-xl bg-surface-1 p-3 shadow-xl"
    >
      <div className="relative z-[3] flex items-start justify-between gap-2 mb-2">
        <div className="min-w-0">
          <h4 className="truncate text-sm font-bold text-on-surface" title={camera.name}>
            {camera.name}
          </h4>
          {siteName && <p className="truncate text-xs text-on-surface-variant">{siteName}</p>}
        </div>

        <div className="flex items-center gap-1 shrink-0">
          <span
            data-state={displayState}
            className={`inline-flex h-6 items-center gap-1.5 rounded-full bg-surface-2 px-2.5 font-mono text-[10px] font-medium uppercase tracking-wider ${STATE_TEXT[displayState]}`}
          >
            <span aria-hidden className={`size-2 rounded-full ${STATE_DOT[displayState]}`} />
            {displayState}
          </span>
          {onPin && (
            <IconButton icon={Pin} onClick={() => onPin(camera.id)} title="Pin preview panel" aria-label="Pin preview" className="size-9" />
          )}
          {onOpenLive && (
            <IconButton icon={ExternalLink} onClick={() => onOpenLive(camera.id)} title="Maximizar en el mapa" aria-label="Maximizar" className="size-9" />
          )}
        </div>
      </div>

      {stage !== "tooltip" && canPreview ? (
        <div className="relative aspect-video w-full overflow-hidden rounded-m3-lg bg-video">
          <img
            key={snapshotUrl}
            src={snapshotUrl}
            alt={camera.name}
            className="absolute inset-0 size-full object-cover"
            loading="eager"
            onLoad={(event) => event.currentTarget.style.removeProperty("display")}
            onError={(e) => {
              (e.currentTarget as HTMLImageElement).style.display = "none";
            }}
          />
          {shouldRenderVideo && (
            <MsePlayer
              cameraId={camera.id}
              quality="sub"
              serverId={camera.serverId}
              persistent={persistent}
              objectFit="cover"
              active
              className="absolute inset-0 size-full bg-transparent"
            />
          )}
        </div>
      ) : (
        <div className="flex h-16 items-center justify-center rounded-m3-lg bg-surface-2 text-xs text-on-surface-variant">
          <Video className="mr-1.5 size-4 opacity-50" />
          <span>Hover to preview</span>
        </div>
      )}

      <div className="mt-2 flex items-center justify-between font-mono text-[11px] text-on-surface-variant">
        <span>{camera.camera.cameraType.toUpperCase()}</span>
        {camera.camera.fovDeg && <span>FOV: {camera.camera.fovDeg}°</span>}
        {camera.camera.rangeM && <span>Range: {camera.camera.rangeM}m</span>}
      </div>
    </div>
  );
}
