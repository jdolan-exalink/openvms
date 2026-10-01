import type { CameraEntity } from "@/lib/maps/types";
import type { HoverStage } from "@/lib/maps/hoverIntent";
import { SurfaceSlot } from "@/lib/live/SurfaceLayer";
import { usePlayerSession } from "@/lib/live/PlayerSessionProvider";
import { STATE_COLORS, computeDisplayState } from "@/lib/maps/entityIndex";
import { ExternalLink, Pin, Video } from "lucide-react";

export interface CameraPreviewProps {
  camera: CameraEntity;
  siteName?: string;
  stage: HoverStage;
  position: { x: number; y: number };
  onPin?: (cameraId: string) => void;
  onOpenLive?: (cameraId: string) => void;
  liveOnHover?: boolean;
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
  canPreview = true,
  onHoverEnter,
  onHoverLeave,
}: CameraPreviewProps) {
  const shouldRenderVideo = stage === "live" && liveOnHover && canPreview;
  const shouldPrewarm = canPreview && (stage === "prewarm" || stage === "live");
  const session = usePlayerSession(shouldPrewarm ? camera.id : "", "sub", camera.serverId);

  const displayState = computeDisplayState(
    camera.status,
    camera.activeAlarms,
    camera.metadata?.serverOffline as boolean | undefined,
  );
  const stateColor = STATE_COLORS[displayState] ?? "#7e8a9a";
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
      className="pointer-events-auto absolute w-72 rounded-lg border border-border bg-card p-3 shadow-xl "
    >
      <div className="relative z-[3] flex items-start justify-between gap-2 mb-2">
        <div className="min-w-0">
          <h4 className="truncate text-sm font-semibold text-ink" title={camera.name}>
            {camera.name}
          </h4>
          {siteName && <p className="truncate text-xs text-muted">{siteName}</p>}
        </div>

        <div className="flex items-center gap-1 shrink-0">
          <span
            style={{ backgroundColor: `${stateColor}20`, color: stateColor, borderColor: stateColor }}
            className="rounded-full border px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider"
          >
            {displayState}
          </span>
          {onPin && (
            <button
              type="button"
              onClick={() => onPin(camera.id)}
              className="rounded p-1 text-muted hover:bg-hover hover:text-ink transition-colors"
              title="Pin preview panel"
              aria-label="Pin preview"
            >
              <Pin className="size-3.5" />
            </button>
          )}
          {onOpenLive && (
            <button
              type="button"
              onClick={() => onOpenLive(camera.id)}
              className="rounded p-1 text-muted hover:bg-hover hover:text-ink transition-colors"
              title="Open in Live View"
              aria-label="Open in Live View"
            >
              <ExternalLink className="size-3.5" />
            </button>
          )}
        </div>
      </div>

      {stage !== "tooltip" && canPreview ? (
        <div className="relative aspect-video w-full overflow-hidden rounded bg-black/40">
          <img
            key={snapshotUrl}
            src={snapshotUrl}
            alt={camera.name}
            className="size-full object-cover"
            loading="eager"
            onLoad={(event) => event.currentTarget.style.removeProperty("display")}
            onError={(e) => {
              (e.currentTarget as HTMLImageElement).style.display = "none";
            }}
          />
          {shouldRenderVideo && (
            <SurfaceSlot session={session} preserveOwner className="absolute inset-0 size-full" />
          )}
        </div>
      ) : (
        <div className="flex h-16 items-center justify-center rounded bg-muted/20 text-xs text-muted">
          <Video className="mr-1.5 size-4 opacity-50" />
          <span>Hover to preview</span>
        </div>
      )}

      <div className="mt-2 flex items-center justify-between text-[11px] text-muted">
        <span>{camera.camera.cameraType.toUpperCase()}</span>
        {camera.camera.fovDeg && <span>FOV: {camera.camera.fovDeg}°</span>}
        {camera.camera.rangeM && <span>Range: {camera.camera.rangeM}m</span>}
      </div>
    </div>
  );
}
