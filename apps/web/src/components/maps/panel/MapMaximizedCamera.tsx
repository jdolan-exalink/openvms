import { Camera, X } from "lucide-react";
import { MsePlayer } from "@/components/MsePlayer";
import { cn } from "@/lib/cn";
import type { CameraEntity } from "@/lib/maps/types";
import { MapGrowFrame, useGrowClose, type GrowRect } from "./MapGrowFrame";

/**
 * Large live tile over the map. Same choice as expanding one Live cell: the detail
 * stream, owned by the shared session when that mode is on, drawn inside the card.
 */
export function MapMaximizedCamera({
  camera,
  origin,
  onClose,
  closeOnEscape = true,
  persistent = false,
}: {
  camera: CameraEntity;
  origin?: GrowRect;
  onClose: () => void;
  closeOnEscape?: boolean;
  persistent?: boolean;
}) {
  const online = camera.status === "online";
  return (
    <MapGrowFrame label={`Cámara maximizada: ${camera.name}`} origin={origin} onClose={onClose} closeOnEscape={closeOnEscape}>
      <div className="relative aspect-video w-full">
        <MsePlayer
          cameraId={camera.id}
          quality="main"
          serverId={camera.serverId}
          persistent={persistent}
          active
          className="absolute inset-0 size-full"
        />
        <div data-map-drag className="absolute inset-x-0 top-0 z-[3] flex cursor-grab items-center gap-1.5 bg-gradient-to-b from-black/80 via-black/45 to-transparent px-3 py-2 text-xs text-white active:cursor-grabbing">
          <Camera className="size-3.5 shrink-0" aria-hidden />
          <span className="truncate font-medium">{camera.name}</span>
          <span className="inline-flex shrink-0 items-center gap-1" role="status" aria-label={`Estado: ${camera.status}`}>
            <span className={cn("size-1.5 rounded-full ring-1 ring-white/80", online ? "bg-emerald-400" : camera.status === "offline" ? "bg-red-400" : "bg-amber-300")} />
          </span>
          <CloseButton />
        </div>
      </div>
    </MapGrowFrame>
  );
}

function CloseButton() {
  const close = useGrowClose();
  return (
    <button type="button" onClick={close} className="ml-auto rounded p-1 hover:bg-white/20" aria-label="Cerrar cámara">
      <X className="size-3.5" aria-hidden />
    </button>
  );
}
