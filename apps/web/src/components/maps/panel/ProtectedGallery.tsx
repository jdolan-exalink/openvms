import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { listProtectedImages, type ProtectedImage } from "@/lib/protectedImages";
import { fmtDateTime } from "@/lib/format";
import { ArPlate } from "@/components/plates/ArPlate";
import { MapGrowFrame, useGrowClose, type GrowRect } from "./MapGrowFrame";

export function ProtectedGallery() {
  const images = useQuery({ queryKey: ["protected-images"], queryFn: listProtectedImages });
  const [open, setOpen] = useState<{ image: ProtectedImage; url: string; origin?: GrowRect }>();
  useEffect(() => () => { if (open) URL.revokeObjectURL(open.url); }, [open]);
  if (images.isLoading) return <p role="status" className="p-2 text-xs text-muted">Cargando imágenes protegidas…</p>;
  if (images.isError) return <p role="alert" className="p-2 text-xs text-bad">No se pudieron abrir las imágenes protegidas.</p>;
  const items = images.data ?? [];
  if (!items.length) return <p className="p-2 text-xs text-muted">Todavía no hay imágenes protegidas. Protegé una alarma o una patente para conservarla acá.</p>;
  return (
    <div>
      <ul className="space-y-2" aria-label="Imágenes protegidas">
        {items.map((item) => (
          <ProtectedCard key={item.id} image={item} onOpen={(url, origin) => setOpen({ image: item, url, origin })} />
        ))}
      </ul>
      {open && (
        <MapGrowFrame label={`Imagen protegida ${open.image.title}`} origin={open.origin} onClose={() => setOpen(undefined)} fixed className="z-40">
          <div className="relative aspect-video w-full bg-black">
            <img src={open.url} alt={open.image.title} className="size-full object-contain" />
            <div data-map-drag className="absolute inset-x-0 top-0 z-[3] flex cursor-grab items-center gap-2 bg-gradient-to-b from-black/80 to-transparent px-3 py-2 text-xs text-white active:cursor-grabbing">
              {open.image.kind === "plate" ? <ArPlate plate={open.image.title} /> : <span className="font-medium">{open.image.title}</span>}
              <span className="min-w-0 truncate text-white/80">{open.image.detail}</span>
              <GalleryClose />
            </div>
          </div>
          {open.image.comment && <p className="bg-surface px-3 py-2 text-xs text-ink">{open.image.comment}</p>}
        </MapGrowFrame>
      )}
    </div>
  );
}

function ProtectedCard({ image, onOpen }: { image: ProtectedImage; onOpen: (url: string, origin?: GrowRect) => void }) {
  const [url, setUrl] = useState("");
  useEffect(() => {
    const next = URL.createObjectURL(image.blob);
    setUrl(next);
    return () => URL.revokeObjectURL(next);
  }, [image]);
  return (
    <li>
      <button
        type="button"
        onClick={(event) => {
          const rect = event.currentTarget.getBoundingClientRect();
          onOpen(url, { left: rect.left, top: rect.top, width: rect.width, height: rect.height });
        }}
        className="flex w-full items-center gap-2 overflow-hidden rounded-xl border border-line bg-bg/40 p-1.5 text-left hover:border-accent/40"
      >
        {url && <img src={url} alt="" className="h-12 w-16 rounded object-cover" />}
        <span className="min-w-0">
          {image.kind === "plate" ? <ArPlate plate={image.title} /> : <span className="block truncate text-xs font-semibold">{image.title}</span>}
          <span className="block truncate text-[10px] text-muted">{image.detail}</span>
          <span className="block text-[10px] text-muted">{fmtDateTime(image.savedAt)}</span>
        </span>
      </button>
    </li>
  );
}

function GalleryClose() {
  const close = useGrowClose();
  return (
    <button type="button" onClick={close} className="ml-auto rounded p-1 hover:bg-white/20" aria-label="Cerrar imagen protegida">
      ×
    </button>
  );
}
