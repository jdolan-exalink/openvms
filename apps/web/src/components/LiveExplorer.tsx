import { useDndContext, useDraggable, useDroppable } from "@dnd-kit/core";
import { Link, useNavigate } from "@tanstack/react-router";
import type { IconDefinition } from "@fortawesome/fontawesome-svg-core";
import {
  faAnglesDown, faAnglesLeft, faAnglesUp, faArrowUpRightFromSquare, faCloud, faCopy, faGear, faMap, faTableCells, faBookmark, faChevronDown, faChevronRight, faClockRotateLeft, faFolder, faFolderOpen,
  faFolderPlus, faFolderTree, faGripVertical, faMagnifyingGlass, faPen, faTrash,
} from "@fortawesome/free-solid-svg-icons";
import { siteIcon as faBuilding, serverIcon as faServer, cameraIcon as faVideo } from "@/lib/inventoryIcons";
import { FontAwesomeIcon } from "@fortawesome/react-fontawesome";
import { createContext, type KeyboardEvent, type MouseEvent, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import type { Schemas } from "@/api/client";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { ContextMenu, type MenuItem } from "@/components/ContextMenu";
import { ErrorNote, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import { buildTree, type Camera, type Folder, type FolderNode, loadPrefs, type ServerNode, savePrefs, treeCameraDropId, treeFolderId, treeRootDropId, treeServerDragId } from "@/lib/explorer";
import { cameraDragId } from "@/lib/liveGrid";
import type { useCameraFolders } from "@/lib/useCameraFolders";

type FolderApi = ReturnType<typeof useCameraFolders>;

/** What the explorer's context menus can do; supplied by the Live route, which owns grid and view state. */
export type ExplorerActions = {
  /** Saved views the user may edit (targets for "Agregar a vista"). */
  editableViews: { id: string; name: string }[];
  addToGrid: (cameraIds: string[]) => void;
  addToView: (viewId: string, cameraIds: string[]) => void;
  canConfigureSites: boolean;
  canConfigureServers: boolean;
  /** servers.config: opens the Frigate camera config editor. */
  canConfigureCameras: boolean;
};

type MenuProps = { onContextMenu: (e: MouseEvent<HTMLElement>) => void; onKeyDown: (e: KeyboardEvent<HTMLElement>) => void; "aria-haspopup": "menu" };
type Bind = (build: () => MenuItem[]) => MenuProps;
type MenuCtxValue = { bind: Bind; actions: ExplorerActions; navigate: ReturnType<typeof useNavigate> };
const MenuCtx = createContext<MenuCtxValue | null>(null);
const useMenuCtx = () => {
  const ctx = useContext(MenuCtx);
  if (!ctx) throw new Error("explorer menu context missing");
  return ctx;
};

/** "Agregar a vista" submenu: the current grid plus every saved view the user can edit. */
function addToViewItem(actions: ExplorerActions, ids: string[]): MenuItem {
  const empty = ids.length === 0;
  return {
    id: "add",
    label: "Agregar a vista",
    icon: faTableCells,
    disabled: empty,
    children: [
      { id: "add-grid", label: "Grilla actual", icon: faTableCells, onSelect: () => actions.addToGrid(ids) },
      { separator: true, id: "add-sep" },
      ...(actions.editableViews.length
        ? actions.editableViews.map((v): MenuItem => ({ id: `add-view-${v.id}`, label: v.name, icon: faBookmark, onSelect: () => actions.addToView(v.id, ids) }))
        : [{ id: "add-none", label: "Sin vistas editables", disabled: true } satisfies MenuItem]),
    ],
  };
}
type DragData = { kind: "camera" | "folder" | "server"; serverId: string };

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

function Section({ title, icon, open, onToggle, count, menu, children }: { title: string; icon: IconDefinition; open: boolean; onToggle: () => void; count?: number; menu?: MenuProps; children: ReactNode }) {
  return (
    <section className="min-w-0 rounded-xl border border-line bg-bg" aria-label={title}>
      <h2>
        <button
          type="button"
          aria-expanded={open}
          onClick={onToggle}
          className="flex w-full items-center gap-1.5 px-3 py-2 text-xs font-semibold uppercase tracking-wide text-muted focus-visible:outline-2 focus-visible:outline-accent"
          {...menu}
          onKeyDown={(event) => menu?.onKeyDown(event)}
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
  onDoublePlace,
  onPlayback,
  canViewRecordings,
  onCollapse,
  views,
  activeViewId,
  onOpenView,
  onEditView,
  onDuplicateView,
  onDeleteView,
  onCreateView,
  canCreateView,
  notice,
  noticeTone = "warn",
  actions,
  error,
}: {
  cameras: Camera[];
  sites: Schemas["Site"][];
  servers: Schemas["Server"][];
  folderApi: FolderApi;
  onPick: (id: string) => void;
  /** Double-click: put the camera in the first empty cell of the current grid. */
  onDoublePlace: (id: string) => void;
  onPlayback: (id: string) => void;
  canViewRecordings: boolean;
  onCollapse: () => void;
  /** Saved views shown inside the camera tree, the way a site lists its views. */
  views: Schemas["View"][];
  activeViewId: string;
  onOpenView: (id: string) => void;
  onEditView: (id: string) => void;
  onDuplicateView: (id: string) => void;
  onDeleteView: (id: string) => void;
  onCreateView: () => void;
  canCreateView: boolean;
  notice: string | null;
  noticeTone?: "ok" | "warn";
  actions: ExplorerActions;
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
  const placedViews = useMemo(() => placeViews(views, tree, sites, query), [views, tree, sites, query]);
  const saveViewItem = (): MenuItem[] => (canCreateView ? [{ id: "save-view", label: "Guardar grilla como vista", icon: faTableCells, onSelect: onCreateView }] : []);

  const navigate = useNavigate();
  const [menu, setMenu] = useState<{ x: number; y: number; items: MenuItem[]; opener: HTMLElement | null } | null>(null);
  // Pointer: open at the cursor. Keyboard (ContextMenu key / Shift+F10): open beside the focused row.
  const bind: Bind = (build) => ({
    "aria-haspopup": "menu",
    onContextMenu: (e) => {
      e.preventDefault();
      e.stopPropagation();
      setMenu({ x: e.clientX, y: e.clientY, items: build(), opener: null });
    },
    onKeyDown: (e) => {
      if (e.key !== "ContextMenu" && !(e.shiftKey && e.key === "F10")) return;
      e.preventDefault();
      e.stopPropagation();
      const r = e.currentTarget.getBoundingClientRect();
      setMenu({ x: r.left + Math.min(r.width, 48), y: r.bottom, items: build(), opener: e.currentTarget });
    },
  });
  const setSiteOpen = (site: { id: string; servers: ServerNode[] }, open: boolean) =>
    update((p) => {
      const closed = { ...p.closed };
      const keys = [`site:${site.id}`, ...site.servers.flatMap((s) => [`srv:${s.id}`, ...s.folders.map((f) => `fld:${f.folder.id}`)])];
      for (const k of keys) closed[k] = !open;
      return { ...p, closed };
    });

  const [creating, setCreating] = useState<string | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<Folder | null>(null);
  const mutationError = folderApi.create.error ?? folderApi.rename.error ?? folderApi.reorder.error ?? error;

  return (
    <MenuCtx.Provider value={{ bind, actions, navigate }}>
    <div className="flex min-h-0 min-w-0 flex-col gap-2 md:flex-1" data-live-sidebar="true" aria-label="Explorador" role="region">
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
        <Section
          title="Cámaras"
          icon={faFolderTree}
          open={prefs.sections.cameras}
          onToggle={() => update((p) => ({ ...p, sections: { ...p.sections, cameras: !p.sections.cameras } }))}
          count={total}
          menu={bind(() => saveViewItem())}
        >
          <nav aria-label="Cámaras" className="flex min-w-0 flex-col text-sm">
            {placedViews.orphans.map((view) => (
              <ViewRow key={view.id} view={view} active={view.id === activeViewId} canCreateView={canCreateView} onOpen={onOpenView} onEdit={onEditView} onDuplicate={onDuplicateView} onDelete={onDeleteView} />
            ))}
            {tree.map((site) => (
              <div key={site.id} className="min-w-0">
                <button
                  type="button"
                  onClick={() => toggle(`site:${site.id}`)}
                  aria-expanded={isOpen(`site:${site.id}`)}
                  title={site.name}
                  className="flex w-full min-w-0 items-center gap-1 py-1 text-left font-medium"
                  {...bind(() => [
                    ...saveViewItem(),
                    ...(actions.canConfigureSites ? [{ id: "cfg", label: "Configurar", icon: faGear, onSelect: () => void navigate({ to: "/sites" }) } satisfies MenuItem] : []),
                    { id: "map", label: "Nuevo mapa", icon: faMap, disabled: true, hint: "Próximamente" },
                    { separator: true, id: "sep" },
                    { id: "expand", label: "Expandir todo", icon: faAnglesDown, onSelect: () => setSiteOpen(site, true) },
                    { id: "collapse", label: "Contraer todo", icon: faAnglesUp, onSelect: () => setSiteOpen(site, false) },
                  ])}
                >
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
                      siteId={site.id}
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
                      onDoublePlace={onDoublePlace}
                      onPlayback={onPlayback}
                      canViewRecordings={canViewRecordings}
                    />
                  ))}
                {isOpen(`site:${site.id}`) &&
                  (placedViews.bySite.get(site.id) ?? []).map((view) => (
                    <ViewRow key={view.id} view={view} active={view.id === activeViewId} canCreateView={canCreateView} indent="ml-3" onOpen={onOpenView} onEdit={onEditView} onDuplicate={onDuplicateView} onDelete={onDeleteView} />
                  ))}
              </div>
            ))}
            {tree.length === 0 && placedViews.orphans.length === 0 && <p className="py-2 text-xs text-muted">{query ? "Sin resultados." : "No hay cámaras visibles."}</p>}
          </nav>
          {notice && (
            <p role="status" className={cn("rounded border px-2 py-1 text-xs", noticeTone === "ok" ? "border-ok/40 bg-ok/10 text-ok" : "border-warn/40 bg-warn/10 text-warn")}>
              {notice}
            </p>
          )}
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
      {menu && <ContextMenu x={menu.x} y={menu.y} items={menu.items} returnFocusTo={menu.opener} onClose={() => setMenu(null)} />}
    </div>
    </MenuCtx.Provider>
  );
}

/** Views sit in the first site of their tenant. Anything without that site stays at the top of the tree. */
function placeViews(views: Schemas["View"][], tree: { id: string }[], sites: Schemas["Site"][], query: string): { bySite: Map<string, Schemas["View"][]>; orphans: Schemas["View"][] } {
  const visible = views.filter((view) => !query || view.name.toLowerCase().includes(query)).sort((a, b) => a.name.localeCompare(b.name, "es"));
  const tenantOf = new Map(sites.map((site) => [site.id, site.tenant_id]));
  const firstSite = new Map<string, string>();
  for (const node of tree) {
    const tenant = tenantOf.get(node.id);
    if (tenant && !firstSite.has(tenant)) firstSite.set(tenant, node.id);
  }
  const bySite = new Map<string, Schemas["View"][]>();
  const orphans: Schemas["View"][] = [];
  for (const view of visible) {
    const siteId = firstSite.get(view.tenant_id);
    if (!siteId) {
      orphans.push(view);
      continue;
    }
    const list = bySite.get(siteId) ?? [];
    list.push(view);
    bySite.set(siteId, list);
  }
  return { bySite, orphans };
}

/** Avigilon's saved-view glyph: a window split into panes, not a folder or a camera. */
function ViewIcon() {
  return (
    <svg viewBox="0 0 16 16" aria-hidden className="size-3.5 shrink-0 text-sky-500">
      <rect x="1.2" y="1.2" width="13.6" height="13.6" rx="1.4" fill="none" stroke="currentColor" strokeWidth="1.3" />
      <path d="M1.2 8h13.6M8 1.2v13.6" stroke="currentColor" strokeWidth="1.3" />
    </svg>
  );
}

function ViewRow({
  view,
  active,
  canCreateView,
  indent = "",
  onOpen,
  onEdit,
  onDuplicate,
  onDelete,
}: {
  view: Schemas["View"];
  active: boolean;
  canCreateView: boolean;
  indent?: string;
  onOpen: (id: string) => void;
  onEdit: (id: string) => void;
  onDuplicate: (id: string) => void;
  onDelete: (id: string) => void;
}) {
  const { bind } = useMenuCtx();
  const detail = [view.shared ? (view.owner_name ? `Compartida por ${view.owner_name}` : "Compartida") : "Privada", view.editable ? "" : "Solo lectura"].filter(Boolean).join(" · ");
  const menu = bind(() => [
    { id: "edit", label: "Editar", icon: faPen, disabled: !view.editable, hint: view.editable ? undefined : "Solo lectura", onSelect: () => onEdit(view.id) },
    { id: "duplicate", label: "Duplicar", icon: faCopy, disabled: !canCreateView, hint: canCreateView ? undefined : "Sin permiso", onSelect: () => onDuplicate(view.id) },
    { id: "delete", label: "Eliminar", icon: faTrash, danger: true, disabled: !view.editable, hint: view.editable ? undefined : "Solo lectura", onSelect: () => onDelete(view.id) },
  ]);
  return (
    <div className={cn(indent, "flex min-w-0 items-center")}>
      <button
        type="button"
        aria-current={active ? "true" : undefined}
        aria-label={view.shared && view.owner_name ? `${view.name} · ${view.owner_name}` : view.name}
        title={`${view.name}. ${detail}. Clic para abrirla.`}
        onClick={() => onOpen(view.id)}
        className={cn("flex min-w-0 flex-1 items-center gap-1.5 rounded px-1.5 py-0.5 text-left hover:bg-raised", active && "bg-raised font-medium")}
        {...menu}
      >
        <ViewIcon />
        <span className="min-w-0 truncate">{view.name}</span>
        {view.shared && <Icon icon={faCloud} className="ml-auto text-sky-400" />}
      </button>
    </div>
  );
}

function ServerBranch({
  server,
  siteId,
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
  onDoublePlace,
  onPlayback,
  canViewRecordings,
}: {
  server: ServerNode;
  siteId: string;
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
  onDoublePlace: (id: string) => void;
  onPlayback: (id: string) => void;
  canViewRecordings: boolean;
}) {
  const key = `srv:${server.id}`;
  const drop = useDropState(server.id, server.canManage, ["camera"]);
  const { setNodeRef, isOver } = useDroppable({ id: treeRootDropId(server.id), disabled: drop.disabled });
  const { setNodeRef: setDragRef, attributes, listeners, isDragging } = useDraggable({
    id: treeServerDragId(server.id),
    data: { kind: "server", serverId: server.id } satisfies DragData,
  });
  const { bind, actions, navigate } = useMenuCtx();
  const cameraIds = [...server.folders.flatMap((f) => f.cameras), ...server.rootCameras].map((c) => c.id);
  const menu = bind(() => [
    addToViewItem(actions, cameraIds),
    ...(actions.canConfigureServers ? [{ id: "cfg", label: "Configurar", icon: faGear, onSelect: () => void navigate({ to: "/servers", search: { site_id: siteId, server_id: server.id } }) } satisfies MenuItem] : []),
  ]);
  return (
    <div className={cn("ml-3 min-w-0", drop.foreign && "opacity-40")} title={drop.foreign ? "No se puede mover entre servidores" : undefined}>
      <div ref={setNodeRef} className={cn("flex min-w-0 items-center gap-1 rounded", isOver && !drop.disabled && "ring-1 ring-accent")}>
        <button
          ref={setDragRef}
          type="button"
          onClick={() => toggle(key)}
          aria-expanded={isOpen(key)}
          title={`${server.name}. Arrastrá a la grilla para ver todas sus cámaras.`}
          className={cn("flex min-w-0 flex-1 cursor-grab items-center gap-1 py-0.5 text-left text-muted", isDragging && "opacity-50")}
          {...attributes}
          {...listeners}
          {...menu}
          onKeyDown={(event) => {
            menu.onKeyDown(event);
            if (!event.defaultPrevented) (listeners?.onKeyDown as ((ev: KeyboardEvent<HTMLElement>) => void) | undefined)?.(event);
          }}
        >
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
              onDoublePlace={onDoublePlace}
              onPlayback={onPlayback}
              canViewRecordings={canViewRecordings}
            />
          ))}
          {server.rootCameras.map((c) => (
            <CameraRow key={c.id} camera={c} canManage={server.canManage} indent="ml-4" onPick={onPick} onDoublePlace={onDoublePlace} onPlayback={onPlayback} canViewRecordings={canViewRecordings} />
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
  onDoublePlace,
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
  onDoublePlace: (id: string) => void;
  onPlayback: (id: string) => void;
  canViewRecordings: boolean;
}) {
  const { folder } = node;
  const key = `fld:${folder.id}`;
  const { bind, actions } = useMenuCtx();
  const drop = useDropState(server.id, server.canManage, ["camera", "folder"]);
  const { setNodeRef: setDropRef, isOver } = useDroppable({ id: treeFolderId(folder.id), disabled: drop.disabled });
  const { setNodeRef: setDragRef, attributes, listeners, isDragging } = useDraggable({
    id: treeFolderId(folder.id),
    data: { kind: "folder", serverId: server.id } satisfies DragData,
  });
  const menu = bind(() => [
    addToViewItem(actions, node.cameras.map((c) => c.id)),
    ...(server.canManage
      ? [
          { separator: true, id: "sep" } satisfies MenuItem,
          { id: "rename", label: "Renombrar", icon: faPen, onSelect: () => onRenaming(folder.id) } satisfies MenuItem,
          { id: "delete", label: "Eliminar", icon: faTrash, danger: true, onSelect: () => onDelete(folder) } satisfies MenuItem,
        ]
      : []),
  ]);
  return (
    <div className={cn("ml-3 min-w-0", drop.foreign && "opacity-40", isDragging && "opacity-50")}>
      <div ref={setDropRef} className={cn("group flex min-w-0 items-center gap-1 rounded", isOver && !drop.disabled && "ring-1 ring-accent")}>
        {server.canManage && (
          <button
            type="button"
            aria-label={`Mover carpeta ${folder.name}`}
            title="Arrastrar para ordenar o soltar en la grilla"
            className="shrink-0 cursor-grab rounded p-0.5 text-muted hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
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
              ref={setDragRef}
              type="button"
              onClick={() => toggle(key)}
              onDoubleClick={server.canManage ? () => onRenaming(folder.id) : undefined}
              aria-expanded={isOpen(key)}
              title={`${folder.name}. Arrastrá a la grilla para ver sus cámaras.`}
              className="flex min-w-0 flex-1 cursor-grab items-center gap-1 py-0.5 text-left"
              {...attributes}
              {...listeners}
              {...menu}
              onKeyDown={(event) => {
                menu.onKeyDown(event);
                if (!event.defaultPrevented) (listeners?.onKeyDown as ((ev: KeyboardEvent<HTMLElement>) => void) | undefined)?.(event);
              }}
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
          <CameraRow key={c.id} camera={c} canManage={server.canManage} indent="ml-5" onPick={onPick} onDoublePlace={onDoublePlace} onPlayback={onPlayback} canViewRecordings={canViewRecordings} />
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
  onDoublePlace,
  onPlayback,
  canViewRecordings,
}: {
  camera: Camera;
  canManage: boolean;
  indent: string;
  onPick: (id: string) => void;
  onDoublePlace: (id: string) => void;
  onPlayback: (id: string) => void;
  canViewRecordings: boolean;
}) {
  const clickTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(clickTimer.current), []);
  const drop = useDropState(camera.server_id, canManage, ["camera"]);
  const { setNodeRef: setDropRef, isOver } = useDroppable({ id: treeCameraDropId(camera.id), disabled: drop.disabled });
  const { bind, actions, navigate } = useMenuCtx();
  const menu = bind(() => [
    addToViewItem(actions, [camera.id]),
    ...(actions.canConfigureCameras
      ? [{ id: "cfg", label: "Configurar", icon: faGear, onSelect: () => void navigate({ to: "/cameras/$cameraId/frigate", params: { cameraId: camera.id } }) } satisfies MenuItem]
      : []),
  ]);
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
        onClick={() => {
          clearTimeout(clickTimer.current);
          clickTimer.current = setTimeout(() => onPick(camera.id), 220);
        }}
        onDoubleClick={() => {
          clearTimeout(clickTimer.current);
          onDoublePlace(camera.id);
        }}
        title={`${camera.display_name}. Doble clic para agregarla a un cuadro libre.`}
        className={cn("flex min-w-0 flex-1 items-center gap-1.5 rounded px-1.5 py-0.5 text-left hover:bg-raised", isDragging && "opacity-50")}
        {...attributes}
        {...listeners}
        {...menu}
        onKeyDown={(e) => {
          menu.onKeyDown(e);
          if (!e.defaultPrevented) (listeners?.onKeyDown as ((ev: KeyboardEvent<HTMLElement>) => void) | undefined)?.(e);
        }}
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
