import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ShieldCheck, X } from "lucide-react";
import { ArPlate } from "@/components/plates/ArPlate";
import { fmtDateTime } from "@/lib/format";
import { listProtectedImages, protectRemoteImage } from "@/lib/protectedImages";
import { MapGrowFrame, useGrowClose, type GrowRect } from "./MapGrowFrame";

export type PlateSnapshotTarget = {
  id: string;
  plate: string;
  cameraName: string;
  seenAt?: string;
  imageUrl: string;
  origin?: GrowRect;
};

/** Floating detection still, grown from the plate notification the same way a camera grows. */
export function MapPlateSnapshot({ target, onClose }: { target: PlateSnapshotTarget; onClose: () => void }) {
  const [failed, setFailed] = useState(false);
  const client = useQueryClient();
  const saved = useQuery({ queryKey: ["protected-images"], queryFn: listProtectedImages });
  const protectedId = `plate:${target.id}`;
  const alreadyProtected = saved.data?.some((item) => item.id === protectedId) ?? false;
  const protect = useMutation({
    mutationFn: () => protectRemoteImage({ id: protectedId, kind: "plate", title: target.plate, detail: target.cameraName, imageUrl: target.imageUrl }),
    onSuccess: () => void client.invalidateQueries({ queryKey: ["protected-images"] }),
  });
  return (
    <MapGrowFrame label={`Detección ${target.plate}`} origin={target.origin} onClose={onClose} className="z-40">
      <div className="relative aspect-video w-full bg-black">
        {failed ? (
          <p className="flex size-full items-center justify-center text-sm text-white/70">Imagen no disponible</p>
        ) : (
          <img
            src={target.imageUrl}
            alt={`Detección del vehículo ${target.plate}`}
            className="size-full object-contain"
            onError={() => setFailed(true)}
          />
        )}
        <div data-map-drag className="absolute inset-x-0 top-0 z-[3] flex cursor-grab items-center gap-2 bg-gradient-to-b from-black/80 via-black/45 to-transparent px-3 py-2 text-xs text-white active:cursor-grabbing">
          <ArPlate plate={target.plate} large />
          <button type="button" disabled={alreadyProtected || protect.isPending} onClick={() => protect.mutate()} className="inline-flex items-center gap-1 rounded bg-black/50 px-1.5 py-0.5 hover:bg-black/70 disabled:opacity-70">
            <ShieldCheck className="size-3.5" aria-hidden />
            {alreadyProtected || protect.isSuccess ? "Protegida" : "Proteger"}
          </button>
          <span className="min-w-0 truncate font-medium">{target.cameraName}</span>
          {target.seenAt && <span className="hidden shrink-0 text-white/70 sm:inline">{fmtDateTime(target.seenAt)}</span>}
          <PlateClose />
        </div>
      </div>
    </MapGrowFrame>
  );
}

function PlateClose() {
  const close = useGrowClose();
  return (
    <button type="button" onClick={close} className="ml-auto rounded p-1 hover:bg-white/20" aria-label="Cerrar detección">
      <X className="size-3.5" aria-hidden />
    </button>
  );
}
