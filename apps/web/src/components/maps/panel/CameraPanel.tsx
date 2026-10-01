import { useMemo, useState } from "react";
import type { CameraEntity, Site } from "@/lib/maps/types";
import { SurfaceSlot } from "@/lib/live/SurfaceLayer";
import { usePlayerSession } from "@/lib/live/PlayerSessionProvider";
import { STATE_COLORS, computeDisplayState } from "@/lib/maps/entityIndex";
import { haversineDistance } from "@/lib/maps/geo";
import {
  Compass,
  ExternalLink,
  Plus,
  Radio,
  X,
  ChevronDown,
  ChevronUp,
} from "lucide-react";

export interface CameraPanelProps {
  pinnedCameras: CameraEntity[];
  allCameras: CameraEntity[];
  sites?: Site[];
  onUnpin: (cameraId: string) => void;
  onSelectCamera: (cameraId: string) => void;
  onOpenLive: (cameraId: string) => void;
  onAddToLive?: (cameraId: string) => void;
  canPreview?: boolean;
}

function SingleCameraCard({
  camera,
  siteName,
  allCameras,
  onUnpin,
  onSelectCamera,
  onOpenLive,
  onAddToLive,
  canPreview = true,
}: {
  camera: CameraEntity;
  siteName?: string;
  allCameras: CameraEntity[];
  onUnpin: (cameraId: string) => void;
  onSelectCamera: (cameraId: string) => void;
  onOpenLive: (cameraId: string) => void;
  onAddToLive?: (cameraId: string) => void;
  canPreview?: boolean;
}) {
  const [showNearby, setShowNearby] = useState(false);
  const session = usePlayerSession(canPreview ? camera.id : "", "sub", camera.serverId);

  const displayState = computeDisplayState(
    camera.status,
    camera.activeAlarms,
    camera.metadata?.serverOffline as boolean | undefined,
  );
  const stateColor = STATE_COLORS[displayState] ?? "#7e8a9a";
  const snapshotUrl = `/media/v1/cameras/${camera.id}/snapshot.jpg?h=240`;

  // Calculate top 4 nearby cameras via haversine distance
  const nearbyCameras = useMemo(() => {
    if (camera.position.kind !== "geo") return [];
    const camCoords: [number, number] = [camera.position.lng, camera.position.lat];

    return allCameras
      .flatMap((c) => {
        if (c.id === camera.id || c.position.kind !== "geo") return [];
        const distM = haversineDistance(camCoords, [c.position.lng, c.position.lat]);
        return [{ camera: c, distanceM: distM }];
      })
      .sort((a, b) => a.distanceM - b.distanceM)
      .slice(0, 4);
  }, [camera, allCameras]);

  const formatDistance = (meters: number) => {
    if (meters < 1000) return `${Math.round(meters)} m`;
    return `${(meters / 1000).toFixed(1)} km`;
  };

  return (
    <div className="flex flex-col rounded-lg border border-line bg-surface shadow-md overflow-hidden text-sm">
      {/* Header */}
      <div className="relative z-[3] flex items-center justify-between border-b border-line bg-muted/30 px-3 py-2">
        <div className="min-w-0 pr-2">
          <div className="flex items-center gap-1.5">
            <span
              style={{ backgroundColor: stateColor }}
              className="inline-block size-2 rounded-full shrink-0"
            />
            <h4 className="truncate font-semibold text-ink" title={camera.name}>
              {camera.name}
            </h4>
          </div>
          {siteName && <p className="truncate text-xs text-muted pl-3.5">{siteName}</p>}
        </div>

        <div className="flex items-center gap-1 shrink-0">
          {canPreview && <button
            type="button"
            onClick={() => onOpenLive(camera.id)}
            className="rounded p-1 text-muted hover:bg-raised hover:text-ink transition-colors"
            title="Open in Live View"
            aria-label="Open in Live View"
          >
            <ExternalLink className="size-4" />
          </button>}
          <button
            type="button"
            onClick={() => onUnpin(camera.id)}
            className="rounded p-1 text-muted hover:bg-raised hover:text-ink transition-colors"
            title="Close preview"
            aria-label="Close preview"
          >
            <X className="size-4" />
          </button>
        </div>
      </div>

      {/* Video stream container with snapshot poster */}
      <div className="relative aspect-video w-full bg-black/60 overflow-hidden">
        {canPreview && <img
          src={snapshotUrl}
          alt={camera.name}
          className="size-full object-cover"
          loading="eager"
          onError={(e) => {
            (e.currentTarget as HTMLImageElement).style.display = "none";
          }}
        />}
        {canPreview && <SurfaceSlot session={session} preserveOwner className="absolute inset-0 size-full" />}
      </div>

      {/* Toolbar & Actions */}
      <div className="relative z-[3] flex flex-wrap items-center justify-between gap-1 p-2 border-b border-line/50 text-xs">
        <div className="flex items-center gap-1.5 text-muted">
          <span>{camera.camera.cameraType.toUpperCase()}</span>
          {camera.camera.fovDeg && (
            <span className="flex items-center gap-0.5">
              <Compass className="size-3" />
              {camera.camera.fovDeg}°
            </span>
          )}
        </div>

        <div className="flex items-center gap-1">
          {onAddToLive && (
            <button
              type="button"
              onClick={() => onAddToLive(camera.id)}
              className="flex items-center gap-1 rounded border border-line px-2 py-0.5 text-ink hover:bg-raised transition-colors"
              title="Add to current Live View grid"
            >
              <Plus className="size-3" />
              <span>Live Grid</span>
            </button>
          )}


        </div>
      </div>

      {/* Nearby Cameras list */}
      {nearbyCameras.length > 0 && (
        <div className="relative z-[3] p-2">
          <button
            type="button"
            onClick={() => setShowNearby(!showNearby)}
            className="flex w-full items-center justify-between text-xs text-muted hover:text-ink transition-colors"
          >
            <span className="flex items-center gap-1 font-medium">
              <Radio className="size-3" />
              Nearby Cameras ({nearbyCameras.length})
            </span>
            {showNearby ? <ChevronUp className="size-3" /> : <ChevronDown className="size-3" />}
          </button>

          {showNearby && (
            <div className="mt-1.5 space-y-1">
              {nearbyCameras.map(({ camera: nearby, distanceM }) => (
                <button
                  key={nearby.id}
                  type="button"
                  onClick={() => onSelectCamera(nearby.id)}
                  className="flex w-full items-center justify-between rounded px-2 py-1 text-xs hover:bg-raised text-left transition-colors"
                >
                  <span className="truncate pr-2 text-ink">{nearby.name}</span>
                  <span className="font-mono text-[10px] text-muted shrink-0">
                    {formatDistance(distanceM)}
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function CameraPanel({
  pinnedCameras,
  allCameras,
  sites = [],
  onUnpin,
  onSelectCamera,
  onOpenLive,
  onAddToLive,
  canPreview = true,
}: CameraPanelProps) {
  if (pinnedCameras.length === 0) return null;

  const siteMap = new Map(sites.map((s) => [s.id, s.name]));

  return (
    <div className="absolute right-4 top-16 flex max-h-[calc(100%-5rem)] w-80 flex-col gap-3 overflow-y-auto pr-1 pointer-events-auto">
      {pinnedCameras.map((camera) => (
        <SingleCameraCard
          key={camera.id}
          camera={camera}
          siteName={siteMap.get(camera.siteId)}
          allCameras={allCameras}
          onUnpin={onUnpin}
          onSelectCamera={onSelectCamera}
          onOpenLive={onOpenLive}
          onAddToLive={onAddToLive}
          canPreview={canPreview}
        />
      ))}
    </div>
  );
}
