import { useState } from "react";
import { Modal } from "@/components/Modal";
import { useT } from "@/i18n";
import { cn } from "@/lib/cn";

/**
 * FaceZoom renders a face crop as a button that opens the same crop enlarged in a dialog. The crop is
 * the only image stored for a sighting, so enlarging it is a display change, not another request. Crops are
 * tiny (about 40 px), so the dialog scales them up with nearest-neighbour rendering: smooth interpolation would
 * only smear a few pixels into a blur, while hard pixel edges stay readable. A smaller dialog avoids stretching them far.
 */
export function FaceZoom({ src, alt, title, className }: { src: string; alt: string; title: string; className?: string }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        type="button"
        aria-label={t("faces.zoomOpen", { title })}
        onClick={() => setOpen(true)}
        className={cn("block cursor-zoom-in overflow-hidden focus-visible:outline-2 focus-visible:outline-primary", className)}
      >
        <img src={src} alt={alt} loading="lazy" className="size-full bg-surface-2 object-cover" />
      </button>
      {open && (
        <Modal title={title} onClose={() => setOpen(false)} className="max-w-sm">
          <img src={src} alt={alt} className="h-[min(60vh,20rem)] w-full rounded-m3-lg bg-surface-2 object-contain [image-rendering:pixelated]" />
        </Modal>
      )}
    </>
  );
}
