import { useState } from "react";
import { createPortal } from "react-dom";
import type { Schemas } from "@/api/client";
import { VehicleFacts } from "@/components/VehicleMark";
import { fmtDateTime } from "@/lib/format";
import { useT } from "@/i18n";
import { ArPlate } from "./ArPlate";

/** Thumb of the tracked-object crop. The full frame stays on the detail and the protected popup. */
export function plateCropUrl(id: string) {
  return `/media/v1/lpr/reads/${encodeURIComponent(id)}/snapshot.jpg?crop=1&quality=55`;
}

/** Same card the map sidebar uses: crop, plate, camera and time. */
export function PlateReadCard({
  read,
  showPhoto = true,
  onClick,
}: {
  read: Schemas["PlateRead"];
  showPhoto?: boolean;
  onClick?: (origin: { left: number; top: number; width: number; height: number }) => void;
}) {
  const t = useT();
  const [preview, setPreview] = useState<{ top: number; left: number } | null>(null);
  const text = read.plate_normalized || read.plate;
  const imageUrl = plateCropUrl(read.id);
  const body = (
    <>
      {showPhoto && <img src={imageUrl} alt={t("plates.readingAlt", { plate: text })} className="h-20 w-full rounded-md bg-black object-contain" />}
      <span className="flex items-center gap-2">
        <ArPlate plate={text} />
        <span className="min-w-0">
          <span className="block truncate text-[11px] font-medium text-ink">{read.camera_name}</span>
          <span className="block text-[10px] text-muted">{fmtDateTime(read.seen_at)}</span>
        </span>
      </span>
      <VehicleFacts labels={read.label ? [read.label] : []} vehicle={read.vehicle} serverName={read.server_name} />
    </>
  );
  const frame = "flex w-full flex-col gap-1.5 rounded-xl border border-line bg-bg/40 p-2 text-left";
  return (
    <li
      onMouseEnter={(event) => {
        if (!showPhoto) return;
        const rect = event.currentTarget.getBoundingClientRect();
        setPreview({ top: rect.top, left: rect.right + 8 });
      }}
      onMouseLeave={() => setPreview(null)}
    >
      {onClick ? (
        <button
          type="button"
          onClick={(event) => {
            const rect = event.currentTarget.getBoundingClientRect();
            onClick({ left: rect.left, top: rect.top, width: rect.width, height: rect.height });
          }}
          className={`${frame} hover:border-accent/40`}
        >
          {body}
        </button>
      ) : (
        <div className={frame}>{body}</div>
      )}
      {showPhoto && preview && createPortal(
        <div className="pointer-events-none fixed z-50 w-48 overflow-hidden rounded-lg border border-white/15 bg-black shadow-2xl" style={{ top: preview.top, left: preview.left }}>
          <img src={imageUrl} alt={t("plates.hoverAlt", { plate: text })} className="max-h-64 w-full object-contain" />
        </div>,
        document.body,
      )}
    </li>
  );
}
