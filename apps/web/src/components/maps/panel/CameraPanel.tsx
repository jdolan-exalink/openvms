import { useState, type PointerEvent as ReactPointerEvent } from "react";
import type { CameraEntity, Site } from "@/lib/maps/types";
import { clampPinnedOrigin, measureMapStage, PINNED_WINDOW_WIDTH, type PinnedWindow } from "@/lib/maps/pinnedWindows";
import { MsePlayer } from "@/components/MsePlayer";
import { computeDisplayState } from "@/lib/maps/entityIndex";
import { IconButton } from "@/components/ui";
import { STATE_DOT } from "../stateTone";
import { ExternalLink, LayoutGrid, X } from "lucide-react";

export interface CameraPanelProps {
  pinnedCameras: CameraEntity[];
  windows?: PinnedWindow[];
  sites?: Site[];
  onUnpin: (cameraId: string) => void;
  onOpenLive: (cameraId: string) => void;
  onMove?: (cameraId: string, x: number, y: number) => void;
  onArrange?: () => void;
  /** Same session owner as Live. The video stays in the card so the map window cannot cover it. */
  persistent?: boolean;
  canPreview?: boolean;
}

function SingleCameraCard({
  camera,
  siteName,
  x,
  y,
  onUnpin,
  onOpenLive,
  onMove,
  onArrange,
  persistent = false,
  canPreview = true,
}: {
  camera: CameraEntity;
  siteName?: string;
  x: number;
  y: number;
  onUnpin: (cameraId: string) => void;
  onOpenLive: (cameraId: string) => void;
  onMove?: (cameraId: string, x: number, y: number) => void;
  onArrange?: () => void;
  persistent?: boolean;
  canPreview?: boolean;
}) {
  const [drag, setDrag] = useState<{ x: number; y: number } | null>(null);
  const displayState = computeDisplayState(
    camera.status,
    camera.activeAlarms,
    camera.metadata?.serverOffline as boolean | undefined,
  );
  const snapshotUrl = `/media/v1/cameras/${camera.id}/snapshot.jpg?h=240`;
  const left = drag?.x ?? x;
  const top = drag?.y ?? y;

  const startDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    if ((event.target as HTMLElement).closest("button")) return;
    event.preventDefault();
    event.stopPropagation();
    const originX = left;
    const originY = top;
    const startX = event.clientX;
    const startY = event.clientY;
    const move = (ev: PointerEvent) => {
      const next = clampPinnedOrigin(originX + ev.clientX - startX, originY + ev.clientY - startY, measureMapStage());
      setDrag(next);
    };
    const up = (ev: PointerEvent) => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      const next = clampPinnedOrigin(originX + ev.clientX - startX, originY + ev.clientY - startY, measureMapStage());
      setDrag(null);
      onMove?.(camera.id, next.x, next.y);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };

  return (
    <div
      data-map-source={camera.id}
      data-map-source-rank="2"
      className="pointer-events-auto absolute flex flex-col overflow-hidden rounded-m3-xl bg-surface-1 text-xs shadow-lg"
      style={{ left, top, width: PINNED_WINDOW_WIDTH }}
    >
      <div
        data-map-drag
        onPointerDown={startDrag}
        className="relative z-[3] flex h-11 cursor-grab items-center gap-1.5 bg-surface-2 pl-3 pr-1 active:cursor-grabbing"
        title={siteName ? `${camera.name} · ${siteName}` : camera.name}
      >
        <span className={`inline-block size-2 shrink-0 rounded-full ${STATE_DOT[displayState]}`} />
        <h4 className="min-w-0 flex-1 truncate font-bold text-on-surface">{camera.name}</h4>
        {onArrange && (
          <IconButton icon={LayoutGrid} onClick={onArrange} title="Ordenar ventanas" aria-label="Ordenar ventanas" size="sm" />
        )}
        {canPreview && (
          <IconButton icon={ExternalLink} onClick={() => onOpenLive(camera.id)} title="Maximizar en el mapa" aria-label="Maximizar" size="sm" />
        )}
        <IconButton icon={X} onClick={() => onUnpin(camera.id)} title="Cerrar" aria-label="Close preview" size="sm" />
      </div>

      <div className="relative aspect-video w-full overflow-hidden bg-video">
        {canPreview && (
          <img
            src={snapshotUrl}
            alt={camera.name}
            className="absolute inset-0 size-full object-cover"
            loading="eager"
            onError={(e) => {
              (e.currentTarget as HTMLImageElement).style.display = "none";
            }}
          />
        )}
        {canPreview && (
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
    </div>
  );
}

export function CameraPanel({
  pinnedCameras,
  windows = [],
  sites = [],
  onUnpin,
  onOpenLive,
  onMove,
  onArrange,
  persistent = false,
  canPreview = true,
}: CameraPanelProps) {
  if (pinnedCameras.length === 0) return null;
  const siteMap = new Map(sites.map((site) => [site.id, site.name]));
  const place = new Map(windows.map((window) => [window.id, window]));

  return (
    <div data-testid="pinned-windows" className="pointer-events-none absolute inset-0 z-10">
      {pinnedCameras.map((camera, index) => {
        const stored = place.get(camera.id);
        return (
          <SingleCameraCard
            key={camera.id}
            camera={camera}
            siteName={siteMap.get(camera.siteId)}
            x={stored?.x ?? 16}
            y={stored?.y ?? 16 + index * 28}
            onUnpin={onUnpin}
            onOpenLive={onOpenLive}
            onMove={onMove}
            onArrange={onArrange}
            persistent={persistent}
            canPreview={canPreview}
          />
        );
      })}
    </div>
  );
}
