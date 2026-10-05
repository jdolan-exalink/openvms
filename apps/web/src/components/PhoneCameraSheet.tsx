import { Building2, Search, Server as ServerGlyph, Video, X, Folder as FolderGlyph } from "lucide-react";
import { useMemo, useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";
import type { Schemas } from "@/api/client";
import { Chevron, Count, dot, nodeIcon, rowBase, rowSelected } from "@/components/ExplorerParts";
import { Icon } from "@/components/Icon";
import { IconButton, TextInput } from "@/components/ui";
import { useT } from "@/i18n";
import { cn } from "@/lib/cn";
import { buildTree, type Camera } from "@/lib/explorer";
import { useFocusTrap } from "@/lib/useFocusTrap";

const row = cn(rowBase, "min-h-11");

/**
 * PhoneCameraSheet is the phone's camera explorer: a modal bottom sheet (scrim, focus trap,
 * Escape and scrim close, like the MobileNav "More" sheet) with search and the same
 * Site > Server > Folder > Camera tree as the desktop explorer, read-only (no drag, no folder
 * editing). "Todas" or a site sets the Live scope; a camera opens its single view.
 */
export function PhoneCameraSheet({
  cameras,
  sites,
  servers,
  folders,
  siteId,
  currentCameraId,
  returnFocusTo,
  onAll,
  onSite,
  onCamera,
  onClose,
}: {
  cameras: Camera[];
  sites: Schemas["Site"][];
  servers: Schemas["Server"][];
  folders: Schemas["CameraFolder"][];
  /** Current scope; null means every site. */
  siteId: string | null;
  currentCameraId?: string;
  returnFocusTo: RefObject<HTMLElement | null>;
  onAll: () => void;
  onSite: (id: string) => void;
  onCamera: (id: string) => void;
  onClose: () => void;
}) {
  const t = useT();
  const ref = useRef<HTMLDivElement>(null);
  const [q, setQ] = useState("");
  useFocusTrap(ref, onClose, returnFocusTo);
  const tree = useMemo(() => buildTree({ cameras, folders, sites, servers, manageable: new Set(), query: q }), [cameras, folders, sites, servers, q]);
  const total = useMemo(() => tree.reduce((n, site) => n + site.count, 0), [tree]);

  const cameraRow = (camera: Camera, indent: string) => (
    <button
      key={camera.id}
      type="button"
      aria-current={camera.id === currentCameraId ? "true" : undefined}
      onClick={() => onCamera(camera.id)}
      className={cn(row, indent, camera.id === currentCameraId && rowSelected)}
    >
      <Icon icon={Video} size="xs" className={nodeIcon} />
      <span className={cn("size-1.5 shrink-0 rounded-full", dot(camera.status))} aria-hidden />
      <span className="min-w-0 truncate">{camera.display_name}</span>
    </button>
  );

  return createPortal(
    <div className="fixed inset-0 z-[90] flex items-end">
      <div data-testid="phone-sheet-scrim" className="absolute inset-0 bg-scrim/50" aria-hidden onMouseDown={onClose} />
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-label={t("live.phoneCamerasSheet")}
        className="relative flex max-h-[85dvh] w-full flex-col gap-3 rounded-t-m3-2xl bg-surface-1 px-4 pt-4 pb-[calc(1rem+env(safe-area-inset-bottom))] shadow-2xl"
      >
        <div className="flex items-center justify-between">
          <p className="text-base font-semibold text-on-surface">{t("live.phoneCamerasSheet")}</p>
          <IconButton icon={X} aria-label={t("live.phoneCloseCameras")} title={t("live.phoneCloseCameras")} onClick={onClose} />
        </div>
        <div className="relative shrink-0">
          <Icon icon={Search} size="xs" className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-on-surface-variant" />
          <TextInput type="search" aria-label={t("live.searchExplorer")} placeholder={t("live.searchPlaceholder")} className="pl-10" value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
        <nav aria-label={t("live.cameras")} className="flex min-h-0 flex-1 flex-col overflow-y-auto overflow-x-hidden text-sm">
          {q.trim() === "" && (
            <button type="button" aria-label={t("live.phoneAllSites")} aria-pressed={siteId === null} onClick={onAll} className={cn(row, "font-bold", siteId === null && rowSelected)}>
              <span className="min-w-0 truncate">{t("live.phoneAllSites")}</span>
              <Count n={total} />
            </button>
          )}
          {tree.map((site) => (
            <div key={site.id} className="min-w-0">
              <button type="button" aria-label={site.name} aria-pressed={siteId === site.id} onClick={() => onSite(site.id)} className={cn(row, "font-bold", siteId === site.id && rowSelected)}>
                <Chevron open />
                <Icon icon={Building2} size="xs" className={nodeIcon} />
                <span className="min-w-0 truncate">{site.name}</span>
                <Count n={site.count} />
              </button>
              {site.servers.map((server) => (
                <div key={server.id} className="ml-3 min-w-0">
                  <p className="flex min-h-8 min-w-0 items-center gap-1.5 px-2 text-on-surface-variant">
                    <Icon icon={ServerGlyph} size="xs" className={nodeIcon} />
                    <span className={cn("size-1.5 shrink-0 rounded-full", dot(server.status))} aria-hidden />
                    <span className="min-w-0 truncate">{server.name}</span>
                  </p>
                  {server.folders.map((node) => (
                    <div key={node.folder.id} className="ml-3 min-w-0">
                      <p className="flex min-h-8 min-w-0 items-center gap-1.5 px-2 text-on-surface-variant">
                        <Icon icon={FolderGlyph} size="xs" className={nodeIcon} />
                        <span className="min-w-0 truncate">{node.folder.name}</span>
                      </p>
                      {node.cameras.map((camera) => cameraRow(camera, "ml-3"))}
                    </div>
                  ))}
                  {server.rootCameras.map((camera) => cameraRow(camera, "ml-3"))}
                </div>
              ))}
            </div>
          ))}
          {tree.length === 0 && <p className="py-2 text-xs text-on-surface-variant">{q.trim() ? t("live.noResults") : t("live.noCameras")}</p>}
        </nav>
      </div>
    </div>,
    document.body,
  );
}
