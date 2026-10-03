import { useT } from "@/i18n";
import { cn } from "@/lib/cn";
import {
  clampSidebarWidth, loadSidebarWidth, maxSidebarWidth, saveSidebarWidth, SIDEBAR_DEFAULT_WIDTH, SIDEBAR_KEYBOARD_STEP, SIDEBAR_MIN_WIDTH,
} from "@/lib/sidebarWidth";
import { createContext, useCallback, useContext, useEffect, useRef, useState, useSyncExternalStore, type CSSProperties, type KeyboardEvent, type PointerEvent, type ReactNode } from "react";

const SidebarTargetContext = createContext(false);
let sidebarTarget: HTMLElement | null = null;
const sidebarTargetListeners = new Set<() => void>();
const getSidebarTarget = () => sidebarTarget;
const subscribeSidebarTarget = (listener: () => void) => {
  sidebarTargetListeners.add(listener);
  return () => sidebarTargetListeners.delete(listener);
};
const setSidebarTarget = (target: HTMLElement | null) => {
  if (sidebarTarget === target) return;
  sidebarTarget = target;
  sidebarTargetListeners.forEach((listener) => listener());
};

/** Read the live shell's sidebar mount point without coupling route state to the shell. */
export function useContextSidebarPortalTarget() {
  const available = useContext(SidebarTargetContext);
  const target = useSyncExternalStore(subscribeSidebarTarget, getSidebarTarget, () => null);
  return { target, available };
}

// Context sidebar collapse (Live explorer): the route owns the state, the shell only renders it.
let sidebarCollapsed = false;
const sidebarCollapsedListeners = new Set<() => void>();
const getSidebarCollapsed = () => sidebarCollapsed;
const subscribeSidebarCollapsed = (listener: () => void) => {
  sidebarCollapsedListeners.add(listener);
  return () => sidebarCollapsedListeners.delete(listener);
};
/** Collapse the shell's context sidebar to zero width (a route must reset it on unmount). */
export function setContextSidebarCollapsed(collapsed: boolean) {
  if (sidebarCollapsed === collapsed) return;
  sidebarCollapsed = collapsed;
  sidebarCollapsedListeners.forEach((listener) => listener());
}

// Context sidebar width (resizable, persisted per browser). Collapsing keeps this value, so
// reopening restores the last width.
let sidebarWidth: number | null = null;
const sidebarWidthListeners = new Set<() => void>();
const getSidebarWidth = () => (sidebarWidth ??= loadSidebarWidth());
const subscribeSidebarWidth = (listener: () => void) => {
  sidebarWidthListeners.add(listener);
  return () => sidebarWidthListeners.delete(listener);
};
function setSidebarWidth(width: number, persist: boolean) {
  const next = clampSidebarWidth(width, window.innerWidth);
  if (next !== sidebarWidth) {
    sidebarWidth = next;
    sidebarWidthListeners.forEach((listener) => listener());
  }
  if (persist) saveSidebarWidth(next);
}

/**
 * Vertical drag handle on the sidebar's right edge. Pointer drags are throttled to one update per
 * animation frame; the keyboard resizes in 16 px steps and double click restores the default.
 */
function SidebarResizeHandle({ width, onDragChange }: { width: number; onDragChange: (dragging: boolean) => void }) {
  const t = useT();
  const drag = useRef<{ startX: number; startWidth: number; frame: number; latest: number } | null>(null);
  const max = maxSidebarWidth(typeof window === "undefined" ? 1600 : window.innerWidth);
  useEffect(() => () => {
    if (drag.current) cancelAnimationFrame(drag.current.frame);
  }, []);
  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    drag.current = { startX: e.clientX, startWidth: width, frame: 0, latest: width };
    onDragChange(true);
  };
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d) return;
    d.latest = d.startWidth + e.clientX - d.startX;
    if (d.frame) return;
    d.frame = requestAnimationFrame(() => {
      d.frame = 0;
      setSidebarWidth(d.latest, false);
    });
  };
  const endDrag = () => {
    const d = drag.current;
    if (!d) return;
    cancelAnimationFrame(d.frame);
    drag.current = null;
    setSidebarWidth(d.latest, true);
    onDragChange(false);
  };
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const step = { ArrowLeft: -SIDEBAR_KEYBOARD_STEP, ArrowRight: SIDEBAR_KEYBOARD_STEP }[e.key];
    if (step !== undefined) setSidebarWidth(width + step, true);
    else if (e.key === "Home") setSidebarWidth(SIDEBAR_MIN_WIDTH, true);
    else if (e.key === "End") setSidebarWidth(max, true);
    else return;
    e.preventDefault();
  };
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={t("common.resizeSidebar")}
      aria-valuenow={width}
      aria-valuemin={SIDEBAR_MIN_WIDTH}
      aria-valuemax={max}
      tabIndex={0}
      title={t("common.resizeHint")}
      data-shell-region="context-sidebar-resize"
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
      onLostPointerCapture={endDrag}
      onDoubleClick={() => setSidebarWidth(SIDEBAR_DEFAULT_WIDTH, true)}
      onKeyDown={onKeyDown}
      className="group absolute inset-y-0 right-0 z-10 hidden w-1.5 cursor-col-resize touch-none justify-center outline-none md:flex"
    >
      <span className="h-full w-0.5 bg-transparent transition-colors group-hover:bg-accent/60 group-focus-visible:bg-accent group-active:bg-accent" aria-hidden />
    </div>
  );
}

// Top bar actions slot: routes portal route-specific controls next to the breadcrumb.
const TopBarTargetContext = createContext(false);
let topBarTarget: HTMLElement | null = null;
const topBarTargetListeners = new Set<() => void>();
const getTopBarTarget = () => topBarTarget;
const subscribeTopBarTarget = (listener: () => void) => {
  topBarTargetListeners.add(listener);
  return () => topBarTargetListeners.delete(listener);
};
const setTopBarTarget = (target: HTMLElement | null) => {
  if (topBarTarget === target) return;
  topBarTarget = target;
  topBarTargetListeners.forEach((listener) => listener());
};

/** Read the top bar's actions mount point; `available` is false outside the shell (e.g. in tests). */
export function useTopBarActionsPortalTarget() {
  const available = useContext(TopBarTargetContext);
  const target = useSyncExternalStore(subscribeTopBarTarget, getTopBarTarget, () => null);
  return { target, available };
}

/** Mount point rendered by the shell's top bar for route-provided actions. */
export function TopBarActionsSlot({ className }: { className?: string }) {
  return <div ref={setTopBarTarget} className={className} data-shell-slot="top-bar-actions" />;
}

interface AppShellProps {
  primaryNav: ReactNode;
  contextSidebar?: ReactNode;
  /** Fit the workspace to the viewport (no page scroll) from the md breakpoint up. */
  fitViewport?: boolean;
  children: ReactNode;
}

export function AppShell({ primaryNav, contextSidebar, fitViewport = false, children }: AppShellProps) {
  const t = useT();
  const collapsed = useSyncExternalStore(subscribeSidebarCollapsed, getSidebarCollapsed, () => false);
  const width = useSyncExternalStore(subscribeSidebarWidth, getSidebarWidth, () => SIDEBAR_DEFAULT_WIDTH);
  const [resizing, setResizing] = useState(false);
  const onDragChange = useCallback((dragging: boolean) => setResizing(dragging), []);
  return (
    <SidebarTargetContext.Provider value={contextSidebar != null}>
      <TopBarTargetContext.Provider value>
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50 focus:rounded-lg focus:bg-surface focus:px-4 focus:py-2 focus:text-sm focus:font-semibold focus:text-ink focus:shadow-lg focus:ring-2 focus:ring-accent"
      >
        {t("common.skipToContent")}
      </a>
      <div className={cn("flex flex-col bg-bg pl-16 md:flex-row md:pl-0", fitViewport ? "h-dvh max-h-dvh min-h-0 overflow-hidden" : "min-h-dvh")} data-shell="openvms">
        <aside aria-label="Primary Nav Rail" className="fixed inset-y-0 left-0 z-40 flex w-16 shrink-0 overflow-y-auto md:static md:z-auto" data-shell-region="primary-nav">
          {primaryNav}
        </aside>
        {contextSidebar != null && (
          <div className={cn("relative shrink-0", !collapsed && "md:h-dvh")} data-shell-region="context-sidebar-frame">
            <aside
              aria-label="Context Sidebar"
              aria-hidden={collapsed || undefined}
              inert={collapsed}
              data-collapsed={collapsed}
              // Capped by the viewport (40%, 480 px) but never below the minimum; see lib/sidebarWidth.
              style={{ "--sidebar-w": `min(${width}px, max(${SIDEBAR_MIN_WIDTH}px, min(480px, 40vw)))` } as CSSProperties}
              className={cn(
                "w-full min-w-0 border-b border-line bg-surface px-3 py-4 md:flex md:h-dvh md:w-(--sidebar-w) md:flex-col md:overflow-y-auto md:overflow-x-hidden md:border-b-0 md:border-r",
                "md:transition-[width,padding,border-color] md:duration-200 motion-reduce:transition-none",
                resizing && "md:transition-none",
                collapsed && "hidden md:block md:w-0 md:overflow-hidden md:border-transparent md:px-0",
              )}
              data-shell-region="context-sidebar"
            >
              <div ref={setSidebarTarget} className="flex min-h-0 min-w-0 flex-col gap-3 md:min-w-[calc(var(--sidebar-w)-1.5rem)] md:flex-1">
                {contextSidebar}
              </div>
            </aside>
            {!collapsed && <SidebarResizeHandle width={width} onDragChange={onDragChange} />}
          </div>
        )}
        <main id="main-content" tabIndex={-1} aria-label="Main Workspace" className={cn("min-w-0 flex-1 px-4 outline-none md:px-8", fitViewport ? "flex min-h-0 flex-col overflow-hidden py-3" : "py-6")} data-shell-region="main-workspace">
          {children}
        </main>
      </div>
      </TopBarTargetContext.Provider>
    </SidebarTargetContext.Provider>
  );
}
