import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { useQuery } from "@tanstack/react-query";
import { listProtectedImages, protectedMedia, type ProtectedImage } from "@/lib/protectedImages";
import { fmtDateTime } from "@/lib/format";
import { ArPlate } from "@/components/plates/ArPlate";
import { useT } from "@/i18n";
import { Chip, IconButton } from "@/components/ui";
import { X } from "lucide-react";
import { MapGrowFrame, useGrowClose, type GrowRect } from "./MapGrowFrame";

type View = "saved" | "full" | "clip";

export function ProtectedGallery() {
  const t = useT();
  const images = useQuery({ queryKey: ["protected-images"], queryFn: listProtectedImages });
  const [open, setOpen] = useState<{ image: ProtectedImage; url: string; origin?: GrowRect }>();
  useEffect(() => () => { if (open) URL.revokeObjectURL(open.url); }, [open]);
  if (images.isLoading) return <p role="status" className="p-2 text-xs text-on-surface-variant">{t("protected.loading")}</p>;
  if (images.isError) return <p role="alert" className="p-2 text-xs text-bad">{t("protected.error")}</p>;
  const items = images.data ?? [];
  if (!items.length) return <p className="p-2 text-xs text-on-surface-variant">{t("protected.empty")}</p>;
  return (
    <div>
      <ul className="space-y-2" aria-label={t("protected.list")}>
        {items.map((item) => (
          <ProtectedCard key={item.id} image={item} onOpen={(url, origin) => setOpen({ image: item, url, origin })} />
        ))}
      </ul>
      {open && createPortal(
        <MapGrowFrame label={t("protected.dialog", { title: open.image.title })} origin={open.origin} onClose={() => setOpen(undefined)} fixed scrim className="z-50">
          <ProtectedViewer key={open.image.id} image={open.image} url={open.url} />
        </MapGrowFrame>,
        document.body,
      )}
    </div>
  );
}

function ProtectedViewer({ image, url }: { image: ProtectedImage; url: string }) {
  const t = useT();
  const media = protectedMedia(image);
  const [view, setView] = useState<View>(media.fullUrl ? "full" : "saved");
  const [missing, setMissing] = useState("");
  const choose = (next: View) => {
    setMissing("");
    setView(next);
  };
  return (
    <>
      <div className="relative flex min-h-[18rem] max-h-[75vh] w-full items-center justify-center bg-video">
        {view === "clip" && media.clipUrl ? (
          missing ? <p className="px-4 text-sm text-on-surface-variant">{missing}</p> : (
            <video controls autoPlay className="max-h-[75vh] w-full" src={media.clipUrl} onError={() => setMissing(t("protected.clipGone"))} />
          )
        ) : view === "full" && media.fullUrl ? (
          missing ? <p className="px-4 text-sm text-on-surface-variant">{missing}</p> : (
            <img src={media.fullUrl} alt={t("protected.fullAlt", { title: image.title })} className="max-h-[75vh] w-full object-contain" onError={() => setMissing(t("protected.fullGone"))} />
          )
        ) : (
          <img src={url} alt={image.title} className="max-h-[75vh] w-full object-contain" />
        )}
        <div data-map-drag className="absolute inset-x-0 top-0 z-[3] flex cursor-grab items-center gap-2 bg-gradient-to-b from-black/80 to-transparent px-3 py-2 text-xs text-white active:cursor-grabbing">
          {image.kind === "plate" ? <ArPlate plate={image.title} /> : <span className="font-bold">{image.title}</span>}
          <span className="min-w-0 truncate text-white/80">{image.detail}</span>
          <GalleryClose />
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2 bg-surface-1 px-3 py-2">
        <ViewButton current={view} id="saved" onChoose={choose}>{t("protected.saved")}</ViewButton>
        {media.fullUrl && <ViewButton current={view} id="full" onChoose={choose}>{t("protected.full")}</ViewButton>}
        {media.clipUrl && <ViewButton current={view} id="clip" onChoose={choose}>{t("protected.clip")}</ViewButton>}
      </div>
      {image.comment && <p className="bg-surface-1 px-3 pb-2 text-xs text-on-surface">{image.comment}</p>}
    </>
  );
}

function ViewButton({ current, id, onChoose, children }: { current: View; id: View; onChoose: (view: View) => void; children: string }) {
  const on = current === id;
  return (
    <Chip selected={on} onChange={() => onChoose(id)} className="text-xs">
      {children}
    </Chip>
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
        className="m3-press flex w-full items-center gap-3 overflow-hidden rounded-m3-lg bg-surface-2 p-2 text-left hover:bg-surface-3 focus-visible:outline-2 focus-visible:outline-primary"
      >
        {url && <img src={url} alt="" className="h-12 w-16 rounded-m3-sm object-cover" />}
        <span className="min-w-0">
          {image.kind === "plate" ? <ArPlate plate={image.title} /> : <span className="block truncate text-xs font-bold">{image.title}</span>}
          <span className="block truncate text-[10px] text-on-surface-variant">{image.detail}</span>
          <span className="block font-mono text-[10px] text-on-surface-variant">{fmtDateTime(image.savedAt)}</span>
        </span>
      </button>
    </li>
  );
}

function GalleryClose() {
  const t = useT();
  const close = useGrowClose();
  return (
    <IconButton icon={X} onClick={close} className="ml-auto size-9 text-white hover:bg-white/20" aria-label={t("protected.close")} />
  );
}
