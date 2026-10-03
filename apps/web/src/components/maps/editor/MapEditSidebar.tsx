import { Search } from "lucide-react";
import { useMemo, useState } from "react";
import { MapCameraTree, type MapTreeFolder, type MapTreeServer } from "../panel/MapCameraTree";
import { DRAG_MIME } from "./UnplacedTray";

export interface MapEditCamera {
  id: string;
  name: string;
  status: string;
  serverId?: string;
  serverName?: string;
  folderId?: string | null;
  placed: boolean;
}

/**
 * Camera tree for map edit mode. Servers and Live folders collapse, and each camera
 * shows whether it already has a position on the active map.
 */
export function MapEditSidebar({
  cameras,
  folders = [],
  servers = [],
  armedId,
  onArm,
}: {
  cameras: readonly MapEditCamera[];
  folders?: readonly MapTreeFolder[];
  servers?: readonly MapTreeServer[];
  armedId?: string;
  onArm: (cameraId: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [draggingId, setDraggingId] = useState<string>();
  const needle = query.trim().toLowerCase();
  const visible = useMemo(
    () => cameras.filter((camera) => !needle || camera.name.toLowerCase().includes(needle) || camera.serverName?.toLowerCase().includes(needle)),
    [cameras, needle],
  );
  const placedCount = cameras.filter((camera) => camera.placed).length;

  return (
    <section
      aria-label="Cámaras del mapa"
      className="pointer-events-auto flex h-full min-h-0 w-full flex-col overflow-hidden"
    >
      <header className="px-1 py-1">
        <h2 className="text-sm font-semibold">Cámaras</h2>
        <p className="text-[11px] text-muted">{placedCount} en el mapa · {cameras.length - placedCount} sin ubicar</p>
      </header>
      <label className="relative mb-2 block">
        <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted" aria-hidden />
        <input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Buscar cámara"
          aria-label="Buscar cámara"
          className="w-full rounded border border-line bg-bg py-1 pl-7 pr-2 text-xs"
        />
      </label>
      <div className="min-h-0 flex-1 overflow-auto">
        <MapCameraTree
          cameras={visible}
          folders={folders}
          servers={servers}
          armedId={armedId}
          onSelect={onArm}
          draggable
          draggingId={draggingId}
          keepEmptyFolders={!needle}
          onDragStart={(id, event) => {
            event.dataTransfer.setData(DRAG_MIME, id);
            event.dataTransfer.effectAllowed = "copy";
            setDraggingId(id);
          }}
          onDragEnd={() => setDraggingId(undefined)}
        />
      </div>
    </section>
  );
}
