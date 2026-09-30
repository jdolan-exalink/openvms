import { useDndContext, useDraggable, useDroppable } from "@dnd-kit/core";
import { Link } from "@tanstack/react-router";
import type { IconDefinition } from "@fortawesome/fontawesome-svg-core";
import {
  faAnglesLeft, faArrowUpRightFromSquare, faBookmark, faBuilding, faChevronDown, faChevronRight, faClockRotateLeft, faFolder, faFolderOpen,
  faFolderPlus, faFolderTree, faGripVertical, faMagnifyingGlass, faPen, faServer, faTrash, faVideo,
} from "@fortawesome/free-solid-svg-icons";
import { FontAwesomeIcon } from "@fortawesome/react-fontawesome";
import { type ReactNode, useCallback, useMemo, useState } from "react";
import type { Schemas } from "@/api/client";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { ErrorNote, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import { buildTree, type Camera, type Folder, type FolderNode, loadPrefs, type ServerNode, savePrefs, treeCameraDropId, treeFolderId, treeRootDropId } from "@/lib/explorer";
import { cameraDragId } from "@/lib/liveGrid";
import type { useCameraFolders } from "@/lib/useCameraFolders";

type FolderApi = ReturnType<typeof useCameraFolders>;
type DragData = { kind: "camera" | "folder"; serverId: string };

/** Drag state seen from one server's subtree: only same-server, manageable nodes accept a drop. */
function useDropState(serverId: string, canManage: boolean, accepts: ("camera" | "folder")[]) {
  const { active } = useDndContext();
  const data = active?.data.current as DragData | undefined;
  const tree = data?.kind === "camera" || data?.kind === "folder";
  const foreign = tree && data.serverId !== serverId;
  const valid = tree && !foreign && accepts.includes(data.kind);
  return { dragging: tree, foreign, disabled: !canManage || !valid };
}

const dot = (status: string) => (status === "online" ? "bg-ok" : status === "offline" ? "bg-bad" : "bg-muted");

/** Every explorer icon is a fixed-width Font Awesome glyph so tree rows align at any depth. */
function Icon({ icon, className }: { icon: IconDefinition; className?: string }) {
  return <FontAwesomeIcon icon={icon} fixedWidth className={cn("shrink-0 text-xs", className)} aria-hidden />;
}

/** Muted node-type glyph (site, server, folder, camera) shown before the name. */
const nodeIcon = "text-muted/80";

function Chevron({ open }: { open: boolean }) {
  return <Icon icon={open ? faChevronDown : faChevronRight} className="text-[10px]" />;
}

function Count({ n }: { n: number }) {
  return <span className="ml-auto shrink-0 rounded-full bg-raised px-1.5 text-[11px] tabular-nums text-muted">{n}</span>;
}

function Section({ title, icon, open, onToggle, count, children }: { title: string; icon: IconDefinition; open: boolean; onToggle: () => void; count?: number; children: ReactNode }) {
  return (
    <section className="min-w-0 rounded-xl border border-line bg-bg" aria-label={title}>
      <h2>
        <button
          type="button"
          aria-expanded={open}
          onClick={onToggle}
          className="flex w-full items-center gap-1.5 px-3 py-2 text-xs font-semibold uppercase tracking-wide text-muted focus-visible:outline-2 focus-visible:outline-accent"
        >
          <Chevron open={open} />
          <Icon icon={icon} />
          <span className="truncate">{title}</span>
          {count !== undefined && <Count n={count} />}
        </button>
      </h2>
      {open && <div className="flex min-w-0 flex-col gap-2 px-2 pb-2">{children}</div>}
    </section>
  );
}

/**
 * LiveExplorer is the unified Live sidebar (LV-13): search, the shared Site > Server > Folder >
 * Camera tree and saved views. It renders inside the Live DndContext, so tree drags and grid drags
 * share one context (tree ids are prefixed; see lib/explorer).
 */
export function LiveExplorer({
  cameras,
  sites,
  servers,
  folderApi,
  onPick,
  onPlayback,
  canViewRecordings,
  onCollapse,
  renderViews,
  viewCount,
  notice,
  error,
}: {
  cameras: Camera[];
  sites: Schemas["Site"][];
  servers: Schemas["Server"][];
  folderApi: FolderApi;
  onPick: (id: string) => void;
  onPlayback: (id: string) => void;
  canViewRecordings: boolean;
  onCollapse: () => void;
  renderViews: (query: string) => ReactNode;
  viewCount: number;
  notice: string | null;
  error?: unknown;
}) {
  const [q, setQ] = useState("");
  const [prefs, setPrefs] = useState(loadPrefs);
  const update = useCallback((fn: (p: typeof prefs) => typeof prefs) => {
    setPrefs((p) => {
      const next = fn(p);
      savePrefs(next);
      return next;
    });
  }, []);
  const query = q.trim().toLowerCase();
  const isOpen = (key: string) => query !== "" || !prefs.closed[key];
  const toggle = (key: string) => update((p) => ({ ...p, closed: { ...p.closed, [key]: !p.closed[key] } }));

  const tree = useMemo(
    () => buildTree({ cameras, folders: folderApi.folders, sites, servers, manageable: folderApi.manageable, query: q }),
    [cameras, folderApi.folders, sites, servers, folderApi.manageable, q],
  );
  const total = tree.reduce((n, s) => n + s.count, 0);

  const [creating, setCreating] = useState<string | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<Folder | null>(null);
  const mutationError = folderApi.create.error ?? folderApi.rename.error ?? folderApi.reorder.error ?? error;

  return (
    <div className="flex min-h-0 min-w-0 flex-col gap-2 md:h-[calc(100dvh-2rem)]" data-live-sidebar="true" aria-label="Explorador" role="region">
      <div className="flex shrink-0 items-center gap-2">
        <h2 className="min-w-0 truncate px-1 text-sm font-semibold">Explorador</h2>
        <button
          type="button"
          onClick={onCollapse}
          aria-label="Ocultar explorador"
          title="Ocultar explorador"
          className="ml-auto rounded p-1 text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
        >
          <Icon icon={faAnglesLeft} className="text-sm" />
        </button>
      </div>
      <div className="relative shrink-0">
        <Icon icon={faMagnifyingGlass} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-muted" />
        <TextInput aria-label="Buscar en el explorador" placeholder="Buscar cámaras, carpetas o vistas" className="pl-8" value={q} onChange={(e) => setQ(e.target.value)} />
      </div>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2 overflow-y-auto overflow-x-hidden">
        <Section title="Cámaras" icon={faFolderTree} open={prefs.sections.cameras} onToggle={() => update((p) => ({ ...p, sections: { ...p.sections, cameras: !p.sections.cameras } }))} count={total}>
          <nav aria-label="Cámaras" className="flex min-w-0 flex-col text-sm">
            {tree.map((site) => (
              <div key={site.id} className="min-w-0">
                <button type="button" onClick={() => toggle(`site:${site.id}`)} aria-expanded={isOpen(`site:${site.id}`)} title={site.name} className="flex w-full min-w-0 items-center gap-1 py-1 text-left font-medium">
                  <Chevron open={isOpen(`site:${site.id}`)} />
                  <Icon icon={faBuilding} className={nodeIcon} />
                  <span className="min-w-0 truncate">{site.name}</span>
                  <Count n={site.count} />
                </button>
                {isOpen(`site:${site.id}`) &&
                  site.servers.map((srv) => (
                    <ServerBranch
                      key={srv.id}
                      server={srv}
                      isOpen={isOpen}
                      toggle={toggle}
                      creating={creating === srv.id}
                      onStartCreate={() => setCreating(srv.id)}
                      onCancelCreate={() => setCreating(null)}
                      onCreate={(name) => folderApi.create.mutate({ serverId: srv.id, name }, { onSuccess: () => setCreating(null) })}
                      renaming={renaming}
                      onRenaming={setRenaming}
                      onRename={(id, name) => folderApi.rename.mutate({ id, name }, { onSuccess: () => setRenaming(null) })}
                      onDelete={setConfirmDelete}
                      onPick={onPick}
                      onPlayback={onPlayback}
                      canViewRecordings={canViewRecordings}
                    />
                  ))}
              </div>
            ))}
            {tree.length === 0 && <p className="py-2 text-xs text-muted">{query ? "Sin resultados." : "No hay cámaras visibles."}</p>}
          </nav>
          {notice && (
            <p role="status" className="rounded border border-warn/40 bg-warn/10 px-2 py-1 text-xs text-warn">
              {notice}
            </p>
          )}
        </Section>
        <Section title="Vistas guardadas" icon={faBookmark} open={prefs.sections.views} onToggle={() => update((p) => ({ ...p, sections: { ...p.sections, views: !p.sections.views } }))} count={viewCount}>
          {renderViews(query)}
        </Section>
        <ErrorNote error={mutationError} />
      </div>
      {confirmDelete && (
        <ConfirmDialog
          title="Eliminar carpeta"
          message={`Se eliminará la carpeta «${confirmDelete.name}». Sus cámaras vuelven a la raíz del servidor; no se borra ninguna cámara.`}
          confirmLabel="Eliminar"
          pending={folderApi.remove.isPending}
          error={folderApi.remove.error}
          onCancel={() => setConfirmDelete(null)}
          onConfirm={() => folderApi.remove.mutate(confirmDelete.id, { onSuccess: () => setConfirmDelete(null) })}
        />
      )}
    </div>
  );
}

function ServerBranch({
  server,
  isOpen,
  toggle,
  creating,
  onStartCreate,
  onCancelCreate,
  onCreate,
  renaming,
  onRenaming,
  onRename,
  onDelete,
  onPick,
  onPlayback,
  canViewRecordings,
}: {
  server: ServerNode;
  isOpen: (key: string) => boolean;
  toggle: (key: string) => void;
  creating: boolean;
  onStartCreate: () => void;
  onCancelCreate: () => void;
  onCreate: (name: string) => void;
  renaming: string | null;
  onRenaming: (id: string | null) => void;
  onRename: (id: string, name: string) => void;
  onDelete: (f: Folder) => void;
  onPick: (id: string) => void;
  onPlayback: (id: string) => void;
  canViewRecordings: boolean;
}) {
  const key = `srv:${server.id}`;
  const drop = useDropState(server.id, server.canManage, ["camera"]);
  const { setNodeRef, isOver } = useDroppable({ id: treeRootDropId(server.id), disabled: drop.disabled });
  return (
    <div className={cn("ml-3 min-w-0", drop.foreign && "opacity-40")} title={drop.foreign ? "No se puede mover entre servidores" : undefined}>
      <div ref={setNodeRef} className={cn("flex min-w-0 items-center gap-1 rounded", isOver && !drop.disabled && "ring-1 ring-accent")}>
        <button type="button" onClick={() => toggle(key)} aria-expanded={isOpen(key)} title={server.name} className="flex min-w-0 flex-1 items-center gap-1 py-0.5 text-left text-muted">
          <Chevron open={isOpen(key)} />
          <Icon icon={faServer} className={nodeIcon} />
          <span className={cn("size-1.5 shrink-0 rounded-full", dot(server.status))} aria-hidden />
          <span className="min-w-0 truncate">{server.name}</span>
          <Count n={server.count} />
        </button>
        {server.canManage && (
          <button
            type="button"
            onClick={onStartCreate}
            aria-label={`Nueva carpeta en ${server.name}`}
            title="Nueva carpeta"
            className="shrink-0 rounded p-1 text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
          >
            <Icon icon={faFolderPlus} />
          </button>
        )}
      </div>
      {isOpen(key) && (
        <>
          {creating && <NameInput label={`Nombre de la nueva carpeta en ${server.name}`} initial="" onSubmit={onCreate} onCancel={onCancelCreate} />}
          {server.folders.map((node) => (
            <FolderBranch
              key={node.folder.id}
              node={node}
              server={server}
              isOpen={isOpen}
              toggle={toggle}
              renaming={renaming === node.folder.id}
              onRenaming={onRenaming}
              onRename={onRename}
              onDelete={onDelete}
              onPick={onPick}
              onPlayback={onPlayback}
              canViewRecordings={canViewRecordings}
            />
          ))}
          {server.rootCameras.map((c) => (
            <CameraRow key={c.id} camera={c} canManage={server.canManage} indent="ml-4" onPick={onPick} onPlayback={onPlayback} canViewRecordings={canViewRecordings} />
          ))}
        </>
      )}
    </div>
  );
}

function FolderBranch({
  node,
  server,
  isOpen,
  toggle,
  renaming,
  onRenaming,
  onRename,
  onDelete,
  onPick,
  onPlayback,
  canViewRecordings,
}: {
  node: FolderNode;
  server: ServerNode;
  isOpen: (key: string) => boolean;
  toggle: (key: string) => void;
  renaming: boolean;
  onRenaming: (id: string | null) => void;
  onRename: (id: string, name: string) => void;
  onDelete: (f: Folder) => void;
  onPick: (id: string) => void;
  onPlayback: (id: string) => void;
  canViewRecordings: boolean;
}) {
  const { folder } = node;
  const key = `fld:${folder.id}`;
  const drop = useDropState(server.id, server.canManage, ["camera", "folder"]);
  const { setNodeRef: setDropRef, isOver } = useDroppable({ id: treeFolderId(folder.id), disabled: drop.disabled });
  const { setNodeRef: setDragRef, attributes, listeners, isDragging } = useDraggable({
    id: treeFolderId(folder.id),
    disabled: !server.canManage,
    data: { kind: "folder", serverId: server.id } satisfies DragData,
  });
  return (
    <div className={cn("ml-3 min-w-0", drop.foreign && "opacity-40", isDragging && "opacity-50")}>
      <div ref={setDropRef} className={cn("group flex min-w-0 items-center gap-1 rounded", isOver && !drop.disabled && "ring-1 ring-accent")}>
        {server.canManage && (
          <button
            ref={setDragRef}
            type="button"
            aria-label={`Mover carpeta ${folder.name}`}
            title="Arrastrar para ordenar"
            className="shrink-0 cursor-grab rounded p-0.5 text-muted hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
            {...attributes}
            {...listeners}
          >
            <Icon icon={faGripVertical} />
          </button>
        )}
        {renaming ? (
          <NameInput label={`Nuevo nombre de ${folder.name}`} initial={folder.name} onSubmit={(name) => onRename(folder.id, name)} onCancel={() => onRenaming(null)} />
        ) : (
          <>
            <button
              type="button"
              onClick={() => toggle(key)}
              onDoubleClick={server.canManage ? () => onRenaming(folder.id) : undefined}
              aria-expanded={isOpen(key)}
              title={folder.name}
              className="flex min-w-0 flex-1 items-center gap-1 py-0.5 text-left"
            >
              <Chevron open={isOpen(key)} />
              <Icon icon={isOpen(key) ? faFolderOpen : faFolder} className={nodeIcon} />
              <span className="min-w-0 truncate">{folder.name}</span>
              <Count n={node.cameras.length} />
            </button>
            {server.canManage && (
              <span className="flex shrink-0 opacity-0 focus-within:opacity-100 group-hover:opacity-100">
                <button type="button" aria-label={`Renombrar carpeta ${folder.name}`} title="Renombrar" onClick={() => onRenaming(folder.id)} className="rounded p-1 text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent">
                  <Icon icon={faPen} />
                </button>
                <button type="button" aria-label={`Eliminar carpeta ${folder.name}`} title="Eliminar" onClick={() => onDelete(folder)} className="rounded p-1 text-muted hover:bg-raised hover:text-bad focus-visible:outline-2 focus-visible:outline-accent">
                  <Icon icon={faTrash} />
                </button>
              </span>
            )}
          </>
        )}
      </div>
      {isOpen(key) &&
        node.cameras.map((c) => (
          <CameraRow key={c.id} camera={c} canManage={server.canManage} indent="ml-5" onPick={onPick} onPlayback={onPlayback} canViewRecordings={canViewRecordings} />
        ))}
    </div>
  );
}

/** CameraRow keeps click-to-place and drag-to-grid; managers can also drop cameras on it to reorder. */
function CameraRow({
  camera,
  canManage,
  indent,
  onPick,
  onPlayback,
  canViewRecordings,
}: {
  camera: Camera;
  canManage: boolean;
  indent: string;
  onPick: (id: string) => void;
  onPlayback: (id: string) => void;
  canViewRecordings: boolean;
}) {
  const drop = useDropState(camera.server_id, canManage, ["camera"]);
  const { setNodeRef: setDropRef, isOver } = useDroppable({ id: treeCameraDropId(camera.id), disabled: drop.disabled });
  const { setNodeRef: setDragRef, attributes, listeners, isDragging } = useDraggable({
    id: cameraDragId(camera.id),
    data: { kind: "camera", serverId: camera.server_id } satisfies DragData,
  });
  return (
    <div
      ref={setDropRef}
      className={cn(indent, "flex min-w-0 items-center gap-1 border-t-2 border-transparent", isOver && !drop.disabled && "border-accent", drop.foreign && "opacity-40")}
    >
      <button
        ref={setDragRef}
        type="button"
        onClick={() => onPick(camera.id)}
        title={camera.display_name}
        className={cn("flex min-w-0 flex-1 items-center gap-1.5 rounded px-1.5 py-0.5 text-left hover:bg-raised", isDragging && "opacity-50")}
        {...attributes}
        {...listeners}
      >
        <Icon icon={faVideo} className={nodeIcon} />
        <span className={cn("size-1.5 shrink-0 rounded-full", dot(camera.status))} aria-hidden />
        <span className="min-w-0 truncate">{camera.display_name}</span>
      </button>
      {canViewRecordings && (
        <>
          <button
            type="button"
            title="Ver grabaciones en vivo"
            aria-label={`Ver grabaciones de ${camera.display_name}`}
            onClick={() => onPlayback(camera.id)}
            className="shrink-0 rounded p-1 text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
          >
            <Icon icon={faClockRotateLeft} />
          </button>
          <Link
            to="/playback"
            search={{ camera: camera.id }}
            title="Abrir página de grabaciones"
            aria-label={`Grabaciones de ${camera.display_name}`}
            className="shrink-0 rounded p-1 text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
          >
            <Icon icon={faArrowUpRightFromSquare} />
          </Link>
        </>
      )}
    </div>
  );
}

/** NameInput is the inline editor for creating and renaming folders: Enter saves, Escape or blur cancels. */
function NameInput({ label, initial, onSubmit, onCancel }: { label: string; initial: string; onSubmit: (name: string) => void; onCancel: () => void }) {
  const [value, setValue] = useState(initial);
  return (
    <form
      className="ml-3 flex min-w-0 flex-1 items-center py-0.5"
      onSubmit={(e) => {
        e.preventDefault();
        const name = value.trim();
        if (name && name !== initial) onSubmit(name);
        else onCancel();
      }}
    >
      <TextInput
        autoFocus
        aria-label={label}
        maxLength={80}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onBlur={onCancel}
        onKeyDown={(e) => {
          if (e.key === "Escape") onCancel();
        }}
        className="py-0.5"
      />
    </form>
  );
}
