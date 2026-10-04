import { useDndContext, useDraggable, useDroppable } from "@dnd-kit/core";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  Bookmark, Building2, ChevronDown, ChevronRight, ChevronsDown, ChevronsUp, Cloud, Copy, ExternalLink, Folder as FolderGlyph, FolderOpen, FolderPlus, FolderTree, GripVertical, History, LayoutGrid,
  type LucideIcon, Map as MapIcon, Pencil, Pin, Search, Server as ServerGlyph, Settings, Trash2, Video,
} from "lucide-react";
import { createContext, type KeyboardEvent, type MouseEvent, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import type { Schemas } from "@/api/client";
import { Icon } from "@/components/Icon";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { ContextMenu, type MenuItem } from "@/components/ContextMenu";
import { ErrorNote, IconButton, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import { buildTree, type Camera, type Folder, type FolderNode, loadPrefs, type ServerNode, savePrefs, treeCameraDropId, treeFolderId, treeRootDropId, treeServerDragId } from "@/lib/explorer";
import { LiveDetectionsPanel, LiveLprPanel, LiveMapsPanel } from "@/components/LiveSidePanels";
import { cameraDragId, type LiveMapRef } from "@/lib/liveGrid";
import { useT } from "@/i18n";
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
function addToViewItem(actions: ExplorerActions, ids: string[], t: ReturnType<typeof useT>): MenuItem {
  const empty = ids.length === 0;
  return {
    id: "add",
    label: t("live.addToView"),
    icon: LayoutGrid,
    disabled: empty,
    children: [
      { id: "add-grid", label: t("live.currentGrid"), icon: LayoutGrid, onSelect: () => actions.addToGrid(ids) },
      { separator: true, id: "add-sep" },
      ...(actions.editableViews.length
        ? actions.editableViews.map((v): MenuItem => ({ id: `add-view-${v.id}`, label: v.name, icon: Bookmark, onSelect: () => actions.addToView(v.id, ids) }))
        : [{ id: "add-none", label: t("live.noEditableViews"), disabled: true } satisfies MenuItem]),
    ],
  };
}
type DragData =
  | { kind: "camera"; serverId: string }
  | { kind: "folder"; serverId: string }
  | { kind: "server"; serverId: string };

/** Drag state seen from one server's subtree: only same-server, manageable nodes accept a drop. */
function useDropState(serverId: string, canManage: boolean, accepts: ("camera" | "folder")[]) {
  const { active } = useDndContext();
  const data = active?.data.current as DragData | undefined;
  const treeData = data?.kind === "camera" || data?.kind === "folder" ? data : undefined;
  const tree = treeData !== undefined;
  const foreign = treeData !== undefined && treeData.serverId !== serverId;
  const valid = treeData !== undefined && !foreign && accepts.includes(treeData.kind);
  return { dragging: tree, foreign, disabled: !canManage || !valid };
}

const dot = (status: string) => (status === "online" ? "bg-ok" : status === "offline" ? "bg-bad" : "bg-muted");

/** Muted node-type glyph (site, server, folder, camera) shown before the name. */
const nodeIcon = "shrink-0 text-on-surface-variant";

function Chevron({ open }: { open: boolean }) {
  return <Icon icon={open ? ChevronDown : ChevronRight} size="xs" className="shrink-0" />;
}

function Count({ n }: { n: number }) {
  return <span className="ml-auto shrink-0 rounded-full bg-surface-3 px-2 text-[11px] font-medium tabular-nums text-on-surface-variant">{n}</span>;
}

function Section({ title, icon, open, onToggle, count, menu, children }: { title: string; icon: LucideIcon; open: boolean; onToggle: () => void; count?: number; menu?: MenuProps; children: ReactNode }) {
  return (
    <section className="min-w-0 rounded-m3-xl bg-surface-1" aria-label={title}>
      <h2>
        <button
          type="button"
          aria-expanded={open}
          onClick={onToggle}
          className="flex w-full items-center gap-1.5 px-3 py-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant focus-visible:outline-2 focus-visible:outline-primary"
          {...menu}
          onKeyDown={(event) => menu?.onKeyDown(event)}
        >
          <Chevron open={open} />
          <Icon icon={icon} size="xs" className="shrink-0" />
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
  pinned,
  onTogglePin,
  canEvents,
  canLpr,
  canMaps,
  onPlaceMap,
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
  /** A pinned explorer stays on screen when the pointer leaves the left edge. */
  pinned: boolean;
  onTogglePin: () => void;
  canEvents: boolean;
  canLpr: boolean;
  canMaps: boolean;
  /** Click or drop a map onto the current grid. */
  onPlaceMap: (map: LiveMapRef) => void;
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
  const t = useT();
  const [q, setQ] = useState("");
  const [tab, setTab] = useState<"cameras" | "maps" | "detections" | "lpr">("cameras");
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
  const saveViewItem = (): MenuItem[] => (canCreateView ? [{ id: "save-view", label: t("live.saveGridAsView"), icon: LayoutGrid, onSelect: onCreateView }] : []);

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
    <div className="flex min-h-0 min-w-0 flex-col gap-2 md:flex-1" data-live-sidebar="true" aria-label={t("live.sidebar")} role="region">
      <div className="flex shrink-0 items-center gap-0.5">
        <div role="tablist" aria-label={t("live.sections")} className="flex min-w-0 flex-1 overflow-x-auto text-[11px] font-medium">
          <SideTab id="cameras" current={tab} onSelect={setTab}>{t("live.cameras")}</SideTab>
          <SideTab id="maps" current={tab} onSelect={setTab}>{t("live.maps")}</SideTab>
          <SideTab id="detections" current={tab} onSelect={setTab}>{t("live.detections")}</SideTab>
          <SideTab id="lpr" current={tab} onSelect={setTab}>{t("maps.lpr")}</SideTab>
        </div>
        <IconButton
          icon={Pin}
          variant={pinned ? "tonal" : "standard"}
          aria-pressed={pinned}
          onClick={onTogglePin}
          aria-label={pinned ? t("live.unpin") : t("live.pin")}
          title={pinned ? t("live.unpin") : t("live.pin")}
          size="sm"
        />
      </div>
      {tab === "cameras" && (
        <div className="relative shrink-0">
          <Icon icon={Search} size="xs" className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-on-surface-variant" />
          <TextInput aria-label={t("live.searchExplorer")} placeholder={t("live.searchPlaceholder")} className="pl-10" value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
      )}
      {tab === "maps" && <LiveMapsPanel sites={sites} canMaps={canMaps} onPlace={onPlaceMap} />}
      {tab === "detections" && <LiveDetectionsPanel canEvents={canEvents} onPick={onPick} />}
      {tab === "lpr" && <LiveLprPanel canLpr={canLpr} onPick={onPick} />}
      {tab === "cameras" && <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2 overflow-y-auto overflow-x-hidden">
        <Section
          title={t("live.cameras")}
          icon={FolderTree}
          open={prefs.sections.cameras}
          onToggle={() => update((p) => ({ ...p, sections: { ...p.sections, cameras: !p.sections.cameras } }))}
          count={total}
          menu={bind(() => saveViewItem())}
        >
          <nav aria-label={t("live.cameras")} className="flex min-w-0 flex-col text-sm">
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
                  className="flex w-full min-w-0 items-center gap-1 rounded-m3-sm py-1.5 text-left font-bold hover:bg-on-surface/8"
                  {...bind(() => [
                    ...saveViewItem(),
                    ...(actions.canConfigureSites ? [{ id: "cfg", label: t("live.configure"), icon: Settings, onSelect: () => void navigate({ to: "/sites" }) } satisfies MenuItem] : []),
                    { id: "map", label: t("live.newMap"), icon: MapIcon, disabled: true, hint: t("live.comingSoon") },
                    { separator: true, id: "sep" },
                    { id: "expand", label: t("live.expandAll"), icon: ChevronsDown, onSelect: () => setSiteOpen(site, true) },
                    { id: "collapse", label: t("live.collapseAll"), icon: ChevronsUp, onSelect: () => setSiteOpen(site, false) },
                  ])}
                >
                  <Chevron open={isOpen(`site:${site.id}`)} />
                  <Icon icon={Building2} size="xs" className={nodeIcon} />
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
            {tree.length === 0 && placedViews.orphans.length === 0 && <p className="py-2 text-xs text-muted">{query ? t("live.noResults") : t("live.noCameras")}</p>}
          </nav>
          {notice && (
            <p role="status" className={cn("rounded-m3-md px-3 py-2 text-xs", noticeTone === "ok" ? "bg-ok/15 text-ok" : "bg-warn/15 text-warn")}>
              {notice}
            </p>
          )}
        </Section>
        <ErrorNote error={mutationError} />
      </div>}
      {confirmDelete && (
        <ConfirmDialog
          title={t("live.deleteFolder")}
          message={t("live.deleteFolderMessage", { name: confirmDelete.name })}
          confirmLabel={t("live.delete")}
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

function SideTab<T extends string>({ id, current, onSelect, children }: { id: T; current: T; onSelect: (tab: T) => void; children: ReactNode }) {
  const selected = current === id;
  return (
    <button
      type="button"
      role="tab"
      aria-selected={selected}
      onClick={() => onSelect(id)}
      className={cn("shrink-0 whitespace-nowrap px-2 py-3 font-bold transition-colors", selected ? "border-b-2 border-primary text-primary" : "text-on-surface-variant hover:text-on-surface")}
    >
      {children}
    </button>
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
    <svg viewBox="0 0 16 16" aria-hidden className="size-4 shrink-0 text-primary">
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
  const t = useT();
  const { bind } = useMenuCtx();
  const detail = [view.shared ? (view.owner_name ? t("live.sharedBy", { name: view.owner_name }) : t("live.shared")) : t("live.privateView"), view.editable ? "" : t("live.readOnly")].filter(Boolean).join(" · ");
  const menu = bind(() => [
    { id: "edit", label: t("live.editItem"), icon: Pencil, disabled: !view.editable, hint: view.editable ? undefined : t("live.readOnly"), onSelect: () => onEdit(view.id) },
    { id: "duplicate", label: t("live.duplicateItem"), icon: Copy, disabled: !canCreateView, hint: canCreateView ? undefined : t("live.noPermission"), onSelect: () => onDuplicate(view.id) },
    { id: "delete", label: t("live.delete"), icon: Trash2, danger: true, disabled: !view.editable, hint: view.editable ? undefined : t("live.readOnly"), onSelect: () => onDelete(view.id) },
  ]);
  return (
    <div className={cn(indent, "flex min-w-0 items-center")}>
      <button
        type="button"
        aria-current={active ? "true" : undefined}
        aria-label={view.shared && view.owner_name ? `${view.name} · ${view.owner_name}` : view.name}
        title={t("live.openViewHint", { name: view.name, detail })}
        onClick={() => onOpen(view.id)}
        className={cn("flex min-w-0 flex-1 items-center gap-1.5 rounded-m3-sm px-2 py-1.5 text-left hover:bg-on-surface/8", active && "bg-secondary-container font-medium text-on-secondary-container")}
        {...menu}
      >
        <ViewIcon />
        <span className="min-w-0 truncate">{view.name}</span>
        {view.shared && <Icon icon={Cloud} size="xs" label={t("live.shared")} className="ml-auto shrink-0 text-primary" />}
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
  const t = useT();
  const { bind, actions, navigate } = useMenuCtx();
  const cameraIds = [...server.folders.flatMap((f) => f.cameras), ...server.rootCameras].map((c) => c.id);
  const menu = bind(() => [
    addToViewItem(actions, cameraIds, t),
    ...(actions.canConfigureServers ? [{ id: "cfg", label: t("live.configure"), icon: Settings, onSelect: () => void navigate({ to: "/servers", search: { site_id: siteId, server_id: server.id } }) } satisfies MenuItem] : []),
  ]);
  return (
    <div className={cn("ml-3 min-w-0", drop.foreign && "opacity-40")} title={drop.foreign ? t("live.cannotMove") : undefined}>
      <div ref={setNodeRef} className={cn("flex min-w-0 items-center gap-1 rounded", isOver && !drop.disabled && "ring-1 ring-primary")}>
        <button
          ref={setDragRef}
          type="button"
          onClick={() => toggle(key)}
          aria-expanded={isOpen(key)}
          title={t("live.serverDragHint", { name: server.name })}
          className={cn("flex min-w-0 flex-1 cursor-grab items-center gap-1 rounded-m3-sm py-1.5 text-left text-on-surface-variant hover:bg-on-surface/8", isDragging && "opacity-50")}
          {...attributes}
          {...listeners}
          {...menu}
          onKeyDown={(event) => {
            menu.onKeyDown(event);
            if (!event.defaultPrevented) (listeners?.onKeyDown as ((ev: KeyboardEvent<HTMLElement>) => void) | undefined)?.(event);
          }}
        >
          <Chevron open={isOpen(key)} />
          <Icon icon={ServerGlyph} size="xs" className={nodeIcon} />
          <span className={cn("size-1.5 shrink-0 rounded-full", dot(server.status))} aria-hidden />
          <span className="min-w-0 truncate">{server.name}</span>
          <Count n={server.count} />
        </button>
        {server.canManage && (
          <IconButton icon={FolderPlus} onClick={onStartCreate}
            aria-label={t("live.newFolderIn", { name: server.name })}
            title={t("live.newFolder")}
            size="sm" />
        )}
      </div>
      {isOpen(key) && (
        <>
          {creating && <NameInput label={t("live.newFolderName", { name: server.name })} initial="" onSubmit={onCreate} onCancel={onCancelCreate} />}
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
  const t = useT();
  const { bind, actions } = useMenuCtx();
  const drop = useDropState(server.id, server.canManage, ["camera", "folder"]);
  const { setNodeRef: setDropRef, isOver } = useDroppable({ id: treeFolderId(folder.id), disabled: drop.disabled });
  const { setNodeRef: setDragRef, attributes, listeners, isDragging } = useDraggable({
    id: treeFolderId(folder.id),
    data: { kind: "folder", serverId: server.id } satisfies DragData,
  });
  const menu = bind(() => [
    addToViewItem(actions, node.cameras.map((c) => c.id), t),
    ...(server.canManage
      ? [
          { separator: true, id: "sep" } satisfies MenuItem,
          { id: "rename", label: t("live.rename"), icon: Pencil, onSelect: () => onRenaming(folder.id) } satisfies MenuItem,
          { id: "delete", label: t("live.delete"), icon: Trash2, danger: true, onSelect: () => onDelete(folder) } satisfies MenuItem,
        ]
      : []),
  ]);
  return (
    <div className={cn("ml-3 min-w-0", drop.foreign && "opacity-40", isDragging && "opacity-50")}>
      <div ref={setDropRef} className={cn("group flex min-w-0 items-center gap-1 rounded", isOver && !drop.disabled && "ring-1 ring-primary")}>
        {server.canManage && (
          <IconButton icon={GripVertical} aria-label={t("live.moveFolder", { name: folder.name })} title={t("live.dragFolder")} size="sm" className="shrink-0 cursor-grab" {...listeners} />
        )}
        {renaming ? (
          <NameInput label={t("live.newName", { name: folder.name })} initial={folder.name} onSubmit={(name) => onRename(folder.id, name)} onCancel={() => onRenaming(null)} />
        ) : (
          <>
            <button
              ref={setDragRef}
              type="button"
              onClick={() => toggle(key)}
              onDoubleClick={server.canManage ? () => onRenaming(folder.id) : undefined}
              aria-expanded={isOpen(key)}
              title={t("live.folderDragHint", { name: folder.name })}
              className="flex min-w-0 flex-1 cursor-grab items-center gap-1 rounded-m3-sm py-1.5 text-left hover:bg-on-surface/8"
              {...attributes}
              {...listeners}
              {...menu}
              onKeyDown={(event) => {
                menu.onKeyDown(event);
                if (!event.defaultPrevented) (listeners?.onKeyDown as ((ev: KeyboardEvent<HTMLElement>) => void) | undefined)?.(event);
              }}
            >
              <Chevron open={isOpen(key)} />
              <Icon icon={isOpen(key) ? FolderOpen : FolderGlyph} size="xs" className={nodeIcon} />
              <span className="min-w-0 truncate">{folder.name}</span>
              <Count n={node.cameras.length} />
            </button>
            {server.canManage && (
              <span className="flex shrink-0 opacity-0 focus-within:opacity-100 group-hover:opacity-100">
                <IconButton icon={Pencil} aria-label={t("live.renameFolder", { name: folder.name })} title={t("live.rename")} onClick={() => onRenaming(folder.id)} size="sm" />
                <IconButton icon={Trash2} aria-label={t("live.deleteFolderNamed", { name: folder.name })} title={t("live.delete")} onClick={() => onDelete(folder)} size="sm" className="hover:text-bad" />
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
  const t = useT();
  const { bind, actions, navigate } = useMenuCtx();
  const menu = bind(() => [
    addToViewItem(actions, [camera.id], t),
    ...(actions.canConfigureCameras
      ? [{ id: "cfg", label: t("live.configure"), icon: Settings, onSelect: () => void navigate({ to: "/cameras/$cameraId/frigate", params: { cameraId: camera.id } }) } satisfies MenuItem]
      : []),
  ]);
  const { setNodeRef: setDragRef, attributes, listeners, isDragging } = useDraggable({
    id: cameraDragId(camera.id),
    data: { kind: "camera", serverId: camera.server_id } satisfies DragData,
  });
  return (
    <div
      ref={setDropRef}
      className={cn(indent, "flex min-w-0 items-center gap-1 border-t-2 border-transparent", isOver && !drop.disabled && "border-primary", drop.foreign && "opacity-40")}
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
        title={t("live.cameraHint", { name: camera.display_name })}
        className={cn("flex min-w-0 flex-1 items-center gap-1.5 rounded-m3-sm px-2 py-1.5 text-left hover:bg-on-surface/8", isDragging && "opacity-50")}
        {...attributes}
        {...listeners}
        {...menu}
        onKeyDown={(e) => {
          menu.onKeyDown(e);
          if (!e.defaultPrevented) (listeners?.onKeyDown as ((ev: KeyboardEvent<HTMLElement>) => void) | undefined)?.(e);
        }}
      >
        <Icon icon={Video} size="xs" className={nodeIcon} />
        <span className={cn("size-1.5 shrink-0 rounded-full", dot(camera.status))} aria-hidden />
        <span className="min-w-0 truncate">{camera.display_name}</span>
      </button>
      {canViewRecordings && (
        <>
          <IconButton icon={History} title={t("live.watchLiveRecordings")} aria-label={t("live.watchRecordingsOf", { name: camera.display_name })} onClick={() => onPlayback(camera.id)} size="sm" />
          <Link
            to="/playback"
            search={{ camera: camera.id }}
            title={t("live.openRecordingsPage")}
            aria-label={t("live.recordingsOf", { name: camera.display_name })}
            className="relative inline-flex size-8 shrink-0 items-center justify-center rounded-full before:absolute before:-inset-1 before:content-[''] text-on-surface-variant hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-primary"
          >
            <Icon icon={ExternalLink} size="xs" />
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
