import { Maximize2 } from "lucide-react";
import type { Schemas } from "@/api/client";
import { SingleCamera } from "@/components/PhoneSingleCamera";
import { IconButton } from "@/components/ui";
import { useT } from "@/i18n";
import { cn } from "@/lib/cn";
import { snapshotUrl, statusDot } from "@/lib/phoneSnapshot";
import { useVisibleSnapshot } from "@/lib/useVisibleSnapshot";

type Camera = Schemas["Camera"];

function Thumb({ camera, active, landscape, onSelect }: { camera: Camera; active: boolean; landscape: boolean; onSelect: () => void }) {
  const tr = useT();
  const { ref, src } = useVisibleSnapshot<HTMLLIElement>(snapshotUrl(camera.id));
  return (
    <li ref={ref} className={cn("shrink-0", landscape ? "w-32" : "w-28")}>
      <button
        type="button"
        aria-label={tr("live.phoneShowCamera", { name: camera.display_name })}
        aria-current={active ? "true" : undefined}
        onClick={onSelect}
        className={cn("relative block aspect-video w-full overflow-hidden rounded-m3-md bg-video outline-none ring-2 focus-visible:ring-primary", active ? "ring-primary" : "ring-transparent")}
      >
        <img src={src} alt="" draggable={false} loading="lazy" className="absolute inset-0 size-full object-cover" />
        <span className="absolute inset-x-0 bottom-0 flex items-center gap-1 bg-black/55 px-1.5 py-0.5 text-[11px] text-white">
          <span className={cn("size-1.5 shrink-0 rounded-full", statusDot(camera.status))} aria-hidden />
          <span className="truncate">{camera.display_name}</span>
        </span>
      </button>
    </li>
  );
}

/**
 * PhoneFocus shows one large live camera (the single-camera player, main over the warm sub) and a
 * strip of snapshots to switch it: below the picture in portrait, beside it in landscape. It is the
 * only live stream of this layout; the strip refreshes only the thumbnails on screen.
 */
export function PhoneFocus({ cameras, activeId, landscape, onSelect, onOpen }: { cameras: Camera[]; activeId: string | undefined; landscape: boolean; onSelect: (id: string) => void; onOpen: (id: string) => void }) {
  const tr = useT();
  const active = cameras.find((camera) => camera.id === activeId) ?? cameras[0];
  if (!active) return null;
  return (
    <div data-testid="phone-focus" className={cn("flex min-h-0 flex-1 gap-2", landscape ? "flex-row" : "flex-col")}>
      <div className={cn("relative min-w-0", landscape ? "min-h-0 flex-1" : "shrink-0")}>
        <SingleCamera key={active.id} camera={active} fill={landscape} />
        <div className="absolute right-1.5 top-1.5 z-10">
          <IconButton icon={Maximize2} size="sm" variant="tonal" aria-label={tr("live.phoneViewCamera", { name: active.display_name })} title={tr("live.phoneViewCamera", { name: active.display_name })} onClick={() => onOpen(active.id)} />
        </div>
      </div>
      <ul
        data-testid="phone-focus-strip"
        aria-label={tr("live.phoneFocusStrip")}
        className={cn("flex gap-1.5", landscape ? "w-32 shrink-0 flex-col overflow-y-auto" : "min-h-0 flex-1 items-start overflow-x-auto pb-1")}
      >
        {cameras.map((camera) => (
          <Thumb key={camera.id} camera={camera} active={camera.id === active.id} landscape={landscape} onSelect={() => onSelect(camera.id)} />
        ))}
      </ul>
    </div>
  );
}
