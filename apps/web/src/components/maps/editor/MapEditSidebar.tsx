import { Search } from "lucide-react";
import { useMemo, useState } from "react";
import { TextInput } from "@/components/ui";
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
  onUnplace,
}: {
  cameras: readonly MapEditCamera[];
  folders?: readonly MapTreeFolder[];
  servers?: readonly MapTreeServer[];
  armedId?: string;
  onArm: (cameraId: string) => void;
  onUnplace?: (cameraId: string) => void;
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
      className="pointer-events-auto flex h-full min-h-0 w-full flex-col overflow-hidden text-xs"
    >
      <header className="flex items-center justify-between px-1 pb-1.5">
        <h2 className="text-xs font-bold text-on-surface uppercase tracking-wide">Cámaras</h2>
        <span className="rounded-full bg-surface-2 px-2 py-0.5 font-mono text-[10px] text-on-surface-variant">
          {placedCount} / {cameras.length}
        </span>
      </header>
      <label className="relative mb-2 block">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-on-surface-variant" aria-hidden />
        <TextInput
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Buscar cámara..."
          aria-label="Buscar cámara"
          className="h-8 pl-8 pr-2 text-xs"
        />
      </label>
      <div className="min-h-0 flex-1 overflow-auto">
        <MapCameraTree
          cameras={visible}
          folders={folders}
          servers={servers}
          armedId={armedId}
          onSelect={onArm}
          onUnplace={onUnplace}
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
