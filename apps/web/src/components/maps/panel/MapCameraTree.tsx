import { FontAwesomeIcon } from "@fortawesome/react-fontawesome";
import { faChevronDown, faChevronRight, faFolder, faFolderOpen } from "@fortawesome/free-solid-svg-icons";
import { cameraIcon, serverIcon } from "@/lib/inventoryIcons";
import { GripVertical, MapPin } from "lucide-react";
import { useMemo, useState, type DragEvent, type ReactNode } from "react";

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

function Chevron({ open }: { open: boolean }) {
  return <FontAwesomeIcon icon={open ? faChevronDown : faChevronRight} className="text-[10px] text-muted" aria-hidden />;
}

/**
 * Collapsible server → folder → camera tree. Folders are the ones created in Live.
 */
export function MapCameraTree({
  cameras,
  folders = [],
  servers = [],
  selectedId,
  armedId,
  onSelect,
  onOpen,
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
  draggable?: boolean;
  draggingId?: string;
  onDragStart?: (id: string, event: DragEvent<HTMLButtonElement>) => void;
  onDragEnd?: () => void;
  keepEmptyFolders?: boolean;
}) {
  const [closed, setClosed] = useState<Record<string, boolean>>({});
  const groups = useMemo(() => groupCameras(cameras, folders, servers, keepEmptyFolders), [cameras, folders, servers, keepEmptyFolders]);
  const toggle = (key: string) => setClosed((current) => ({ ...current, [key]: !current[key] }));
  if (groups.length === 0) return <p className="px-1 text-xs text-muted">No hay cámaras para este filtro.</p>;
  return (
    <div className="space-y-1">
      {groups.map((group) => {
        const serverKey = `srv:${group.key}`;
        const serverOpen = !closed[serverKey];
        const count = group.cameras.length + group.folders.reduce((total, folder) => total + folder.cameras.length, 0);
        return (
          <section key={group.key} aria-label={group.name}>
            <h3>
              <button type="button" aria-expanded={serverOpen} onClick={() => toggle(serverKey)} className="flex w-full items-center gap-1.5 rounded px-1 py-1 text-left text-[11px] font-semibold uppercase tracking-wide text-muted hover:bg-raised">
                <Chevron open={serverOpen} />
                <FontAwesomeIcon icon={serverIcon} fixedWidth aria-hidden />
                <span className="min-w-0 flex-1 truncate">{group.name}</span>
                <span className="rounded-full bg-raised px-1.5 text-[10px] tabular-nums">{count}</span>
              </button>
            </h3>
            {serverOpen && (
              <div className="ml-2 space-y-1 border-l border-white/10 pl-1">
                {group.folders.map((folder) => {
                  const folderKey = `fld:${folder.id}`;
                  const folderOpen = !closed[folderKey];
                  return (
                    <div key={folder.id}>
                      <button type="button" aria-expanded={folderOpen} onClick={() => toggle(folderKey)} className="flex w-full items-center gap-1.5 rounded px-1 py-1 text-left text-xs text-ink hover:bg-raised">
                        <Chevron open={folderOpen} />
                        <FontAwesomeIcon icon={folderOpen ? faFolderOpen : faFolder} fixedWidth className="text-muted" aria-hidden />
                        <span className="min-w-0 flex-1 truncate">{folder.name}</span>
                        <span className="text-[10px] tabular-nums text-muted">{folder.cameras.length}</span>
                      </button>
                      {folderOpen && <ul className="ml-4 space-y-1">{folder.cameras.map((camera) => <CameraRow key={camera.id} camera={camera} selectedId={selectedId} armedId={armedId} onSelect={onSelect} onOpen={onOpen} draggable={draggable} draggingId={draggingId} onDragStart={onDragStart} onDragEnd={onDragEnd} />)}</ul>}
                    </div>
                  );
                })}
                {group.cameras.length > 0 && <ul className="space-y-1">{group.cameras.map((camera) => <CameraRow key={camera.id} camera={camera} selectedId={selectedId} armedId={armedId} onSelect={onSelect} onOpen={onOpen} draggable={draggable} draggingId={draggingId} onDragStart={onDragStart} onDragEnd={onDragEnd} />)}</ul>}
              </div>
            )}
          </section>
        );
      })}
    </div>
  );
}

function CameraRow({
  camera,
  selectedId,
  armedId,
  onSelect,
  onOpen,
  draggable,
  draggingId,
  onDragStart,
  onDragEnd,
}: {
  camera: MapTreeCamera;
  selectedId?: string;
  armedId?: string;
  onSelect: (id: string) => void;
  onOpen?: (id: string) => void;
  draggable: boolean;
  draggingId?: string;
  onDragStart?: (id: string, event: DragEvent<HTMLButtonElement>) => void;
  onDragEnd?: () => void;
}) {
  const armed = armedId === camera.id;
  const selected = selectedId === camera.id || armed;
  const placed = camera.placed === true;
  let extra: ReactNode = null;
  if (camera.placed !== undefined) {
    extra = placed
      ? <span aria-hidden className="inline-flex items-center gap-1 rounded-full bg-ok/15 px-1.5 py-0.5 text-[10px] font-semibold text-ok"><MapPin className="size-2.5" aria-hidden />En el mapa</span>
      : <span aria-hidden className="text-[10px] text-muted">Sin ubicar</span>;
  }
  return (
    <li className="flex items-stretch gap-1">
      <button
        type="button"
        draggable={draggable}
        aria-pressed={selected}
        aria-label={camera.name}
        onClick={() => onSelect(camera.id)}
        onDragStart={draggable ? (event) => onDragStart?.(camera.id, event) : undefined}
        onDragEnd={draggable ? () => onDragEnd?.() : undefined}
        className={
          draggingId === camera.id
            ? "flex min-w-0 flex-1 cursor-grabbing items-center gap-2 rounded-lg border border-accent bg-accent/10 px-2 py-1.5 text-left opacity-60"
            : selected
              ? `flex min-w-0 flex-1 items-center gap-2 rounded-lg border px-2 py-1.5 text-left ${placed ? "border-ok bg-ok/15" : "border-accent bg-accent/15"} ${draggable ? "cursor-grab" : ""}`
              : `flex min-w-0 flex-1 items-center gap-2 rounded-lg border px-2 py-1.5 text-left hover:bg-raised ${placed ? "border-ok/70 bg-ok/10" : "border-dashed border-line"} ${draggable ? "cursor-grab" : ""}`
        }
      >
        <FontAwesomeIcon icon={cameraIcon} fixedWidth className="text-muted" aria-hidden />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm">{camera.name}</span>
          {extra}
        </span>
        <span className={camera.status === "online" ? "size-1.5 rounded-full bg-ok" : camera.status === "offline" ? "size-1.5 rounded-full bg-bad" : "size-1.5 rounded-full bg-muted"} aria-hidden />
        {draggable && <GripVertical className="size-3.5 shrink-0 text-muted" aria-hidden />}
      </button>
      {onOpen && (
        <button type="button" aria-label={`Maximizar: ${camera.name}`} onClick={() => onOpen(camera.id)} className="shrink-0 rounded border border-line px-1.5 text-[10px] text-muted hover:bg-raised">
          Abrir
        </button>
      )}
    </li>
  );
}
