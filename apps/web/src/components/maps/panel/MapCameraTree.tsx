import { Folder, FolderOpen, GripVertical, Maximize2, MapPin, Minimize2, Server, Video, X } from "lucide-react";
import { Icon } from "@/components/Icon";
import { Chevron, Count, dot, nodeIcon, rowBase, rowSelected } from "@/components/ExplorerParts";
import { IconButton } from "@/components/ui";
import { cn } from "@/lib/cn";
import { useT } from "@/i18n";
import { useMemo, useState, type DragEvent } from "react";

export type MapTreeCamera = {
  id: string;
  name: string;
  status: string;
  serverId?: string;
  serverName?: string;
  folderId?: string | null;
  placed?: boolean;
};

export type MapTreeFolder = { id: string; name: string; serverId: string; sortOrder?: number };
export type MapTreeServer = { id: string; name: string };

type ServerGroup = {
  key: string;
  name: string;
  folders: { id: string; name: string; cameras: MapTreeCamera[] }[];
  cameras: MapTreeCamera[];
};

function groupCameras(cameras: readonly MapTreeCamera[], folders: readonly MapTreeFolder[], servers: readonly MapTreeServer[], keepEmptyFolders: boolean): ServerGroup[] {
  const serverName = new Map(servers.map((server) => [server.id, server.name]));
  const groups = new Map<string, ServerGroup>();
  const ensure = (key: string, name: string) => {
    const current = groups.get(key);
    if (current) return current;
    const created: ServerGroup = { key, name, folders: [], cameras: [] };
    groups.set(key, created);
    return created;
  };
  for (const folder of [...folders].sort((a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0) || a.name.localeCompare(b.name))) {
    const name = serverName.get(folder.serverId) ?? "Sin servidor";
    const group = ensure(folder.serverId, name);
    if (!group.folders.some((item) => item.id === folder.id)) group.folders.push({ id: folder.id, name: folder.name, cameras: [] });
  }
  for (const camera of cameras) {
    const key = camera.serverId || camera.serverName || "sin-servidor";
    const name = camera.serverName || serverName.get(camera.serverId ?? "") || "Sin servidor";
    const group = ensure(key, name);
    const folder = camera.folderId ? group.folders.find((item) => item.id === camera.folderId) : undefined;
    if (folder) folder.cameras.push(camera);
    else group.cameras.push(camera);
  }
  return [...groups.values()]
    .map((group) => ({
      ...group,
      folders: group.folders.filter((folder) => keepEmptyFolders || folder.cameras.length > 0),
    }))
    .filter((group) => group.cameras.length > 0 || group.folders.length > 0)
    .sort((a, b) => a.name.localeCompare(b.name));
}

type RowShared = {
  selectedId?: string;
  armedId?: string;
  onSelect: (id: string) => void;
  onOpen?: (id: string) => void;
  onClose?: (id: string) => void;
  onUnplace?: (id: string) => void;
  openIds?: ReadonlySet<string>;
  draggable: boolean;
  draggingId?: string;
  onDragStart?: (id: string, event: DragEvent<HTMLButtonElement>) => void;
  onDragEnd?: () => void;
};

/**
 * Collapsible server → folder → camera tree. Folders are the ones created in Live. Row anatomy and
 * density mirror the Live explorer (shared pieces live in ExplorerParts).
 */
export function MapCameraTree({
  cameras,
  folders = [],
  servers = [],
  selectedId,
  armedId,
  onSelect,
  onOpen,
  onClose,
  onUnplace,
  openIds,
  draggable = false,
  draggingId,
  onDragStart,
  onDragEnd,
  keepEmptyFolders = true,
}: {
  cameras: readonly MapTreeCamera[];
  folders?: readonly MapTreeFolder[];
  servers?: readonly MapTreeServer[];
  selectedId?: string;
  armedId?: string;
  onSelect: (id: string) => void;
  onOpen?: (id: string) => void;
  /** Closes an already open live window; when omitted the toggle only opens. */
  onClose?: (id: string) => void;
  /** Unplaces a camera from the active map. */
  onUnplace?: (id: string) => void;
  /** Cameras whose live window is open on the map. */
  openIds?: ReadonlySet<string>;
  draggable?: boolean;
  draggingId?: string;
  onDragStart?: (id: string, event: DragEvent<HTMLButtonElement>) => void;
  onDragEnd?: () => void;
  keepEmptyFolders?: boolean;
}) {
  const [closed, setClosed] = useState<Record<string, boolean>>({});
  const groups = useMemo(() => groupCameras(cameras, folders, servers, keepEmptyFolders), [cameras, folders, servers, keepEmptyFolders]);
  const toggle = (key: string) => setClosed((current) => ({ ...current, [key]: !current[key] }));
  if (groups.length === 0) return <p className="px-1 text-xs text-on-surface-variant">No hay cámaras para este filtro.</p>;
  const shared: RowShared = { selectedId, armedId, onSelect, onOpen, onClose, onUnplace, openIds, draggable, draggingId, onDragStart, onDragEnd };
  return (
    <nav aria-label="Cámaras" className="flex min-w-0 flex-col text-sm">
      {groups.map((group) => {
        const serverKey = `srv:${group.key}`;
        const serverOpen = !closed[serverKey];
        const count = group.cameras.length + group.folders.reduce((total, folder) => total + folder.cameras.length, 0);
        return (
          <section key={group.key} aria-label={group.name} className="min-w-0">
            <h3>
              <button type="button" aria-expanded={serverOpen} onClick={() => toggle(serverKey)} title={group.name} className="flex w-full min-w-0 items-center gap-1 rounded-m3-sm py-1.5 text-left font-bold hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-primary">
                <Chevron open={serverOpen} />
                <Icon icon={Server} size="xs" className={nodeIcon} />
                <span className="min-w-0 truncate">{group.name}</span>
                <Count n={count} />
              </button>
            </h3>
            {serverOpen && (
              <>
                {group.folders.map((folder) => {
                  const folderKey = `fld:${folder.id}`;
                  const folderOpen = !closed[folderKey];
                  return (
                    <div key={folder.id} className="ml-3 min-w-0">
                      <button type="button" aria-expanded={folderOpen} onClick={() => toggle(folderKey)} className="flex w-full min-w-0 items-center gap-1 rounded-m3-sm py-1.5 text-left text-on-surface-variant hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-primary">
                        <Chevron open={folderOpen} />
                        <Icon icon={folderOpen ? FolderOpen : Folder} size="xs" className={nodeIcon} />
                        <span className="min-w-0 truncate">{folder.name}</span>
                        <Count n={folder.cameras.length} />
                      </button>
                      {folderOpen && folder.cameras.map((camera) => <CameraRow key={camera.id} camera={camera} indent="ml-5" shared={shared} />)}
                    </div>
                  );
                })}
                {group.cameras.map((camera) => <CameraRow key={camera.id} camera={camera} indent="ml-4" shared={shared} />)}
              </>
            )}
          </section>
        );
      })}
    </nav>
  );
}

function CameraRow({ camera, indent, shared }: { camera: MapTreeCamera; indent: string; shared: RowShared }) {
  const t = useT();
  const { selectedId, armedId, onSelect, onOpen, onClose, onUnplace, openIds, draggable, draggingId, onDragStart, onDragEnd } = shared;
  const selected = selectedId === camera.id || armedId === camera.id;
  const placed = camera.placed === true;
  const windowOpen = openIds?.has(camera.id) === true;
  const toggleLabel = t(windowOpen ? "maps.closeLiveNamed" : "maps.openLiveNamed", { name: camera.name });
  const canToggle = onOpen && camera.placed !== false;
  return (
    <div className={cn(indent, "group/cam flex min-w-0 items-center gap-1")}>
      <button
        type="button"
        draggable={draggable}
        aria-pressed={selected}
        aria-label={camera.name}
        onClick={() => onSelect(camera.id)}
        onDragStart={draggable ? (event) => onDragStart?.(camera.id, event) : undefined}
        onDragEnd={draggable ? () => onDragEnd?.() : undefined}
        className={cn(rowBase, selected && rowSelected, draggable && "cursor-grab", draggingId === camera.id && "cursor-grabbing opacity-50")}
      >
        <Icon icon={Video} size="xs" className={nodeIcon} />
        <span className={cn("size-1.5 shrink-0 rounded-full", dot(camera.status))} aria-hidden />
        <span className="min-w-0 truncate">{camera.name}</span>
        {placed && (
          <span title={t("maps.onMap")} className="inline-flex shrink-0">
            <Icon icon={MapPin} size="xs" label={t("maps.onMap")} className="text-primary" />
          </span>
        )}
        {draggable && <Icon icon={GripVertical} size="xs" className="ml-auto shrink-0 text-on-surface-variant" />}
      </button>
      {placed && onUnplace && (
        <IconButton
          icon={X}
          size="sm"
          aria-label="Quitar de mapa"
          title="Quitar cámara del mapa"
          className="size-6 shrink-0 text-on-surface-variant hover:bg-bad/10 hover:text-bad"
          onClick={() => onUnplace(camera.id)}
        />
      )}
      {canToggle && (
        <IconButton
          icon={windowOpen ? Minimize2 : Maximize2}
          size="sm"
          aria-label={toggleLabel}
          title={toggleLabel}
          aria-pressed={windowOpen}
          onClick={() => (windowOpen && onClose ? onClose(camera.id) : onOpen(camera.id))}
        />
      )}
    </div>
  );
}
