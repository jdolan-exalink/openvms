import type { Schemas } from "@/api/client";
import { useT } from "@/i18n";
import { cn } from "@/lib/cn";
import { snapshotUrl, statusDot } from "@/lib/phoneSnapshot";
import { useVisibleSnapshot } from "@/lib/useVisibleSnapshot";

type Camera = Schemas["Camera"];

/** One camera as a snapshot: name, status and an offline mark; it refreshes only while on screen. */
function SnapshotTile({ camera, onOpen }: { camera: Camera; onOpen: () => void }) {
  const tr = useT();
  const { ref, src } = useVisibleSnapshot<HTMLLIElement>(snapshotUrl(camera.id));
  return (
    <li ref={ref} className="min-w-0">
      <div className="relative aspect-video w-full overflow-hidden rounded-m3-md bg-video">
        <img src={src} alt="" draggable={false} loading="lazy" className={cn("absolute inset-0 size-full object-cover", camera.status === "offline" && "opacity-40")} />
        {camera.status === "offline" && <span className="absolute inset-0 flex items-center justify-center text-xs font-medium text-white/90">{tr("live.phoneOffline")}</span>}
        <div className="pointer-events-none absolute inset-x-0 bottom-0 flex items-center gap-1.5 p-1.5 text-xs">
          <span className="flex min-w-0 items-center gap-1.5 rounded-full bg-surface-1/80 px-2 py-0.5 text-on-surface backdrop-blur">
            <span className={cn("size-2 shrink-0 rounded-full", statusDot(camera.status))} aria-hidden />
            <span className="truncate font-medium">{camera.display_name}</span>
          </span>
        </div>
        <button type="button" aria-label={tr("live.phoneViewCamera", { name: camera.display_name })} onClick={onOpen} className="absolute inset-0 z-[4] min-h-11 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary" />
      </div>
    </li>
  );
}

/**
 * PhoneGrid is the default multi-camera layout: every camera in scope as a snapshot tile in a
 * vertically scrolling grid (2 columns, 4 in landscape). No live stream is opened here; a tap opens
 * the single camera.
 */
export function PhoneGrid({ cameras, landscape, onOpen }: { cameras: Camera[]; landscape: boolean; onOpen: (id: string) => void }) {
  return (
    <div data-testid="phone-grid" className="min-h-0 flex-1 overflow-y-auto">
      <ul data-testid="phone-grid-list" className={cn("grid gap-1.5", landscape ? "grid-cols-4" : "grid-cols-2")}>
        {cameras.map((camera) => (
          <SnapshotTile key={camera.id} camera={camera} onOpen={() => onOpen(camera.id)} />
        ))}
      </ul>
    </div>
  );
}
