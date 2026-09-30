import { cn } from "@/lib/cn";
import { createContext, useContext, useSyncExternalStore, type ReactNode } from "react";

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
  const collapsed = useSyncExternalStore(subscribeSidebarCollapsed, getSidebarCollapsed, () => false);
  return (
    <SidebarTargetContext.Provider value={contextSidebar != null}>
      <TopBarTargetContext.Provider value>
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50 focus:rounded-lg focus:bg-surface focus:px-4 focus:py-2 focus:text-sm focus:font-semibold focus:text-ink focus:shadow-lg focus:ring-2 focus:ring-accent"
      >
        Saltar al contenido
      </a>
      <div className={cn("flex min-h-dvh flex-col bg-bg md:flex-row", fitViewport && "md:h-dvh md:min-h-0 md:overflow-hidden")} data-shell="openvms">
        <aside aria-label="Primary Nav Rail" className="hidden w-16 shrink-0 md:flex" data-shell-region="primary-nav">
          {primaryNav}
        </aside>
        {contextSidebar != null && (
          <aside
            aria-label="Context Sidebar"
            aria-hidden={collapsed || undefined}
            inert={collapsed}
            data-collapsed={collapsed}
            className={cn(
              "w-full shrink-0 border-b border-line bg-surface px-3 py-4 md:h-dvh md:w-64 md:overflow-y-auto md:border-b-0 md:border-r",
              "md:transition-[width,padding,border-color] md:duration-200 motion-reduce:transition-none",
              collapsed && "hidden md:block md:w-0 md:overflow-hidden md:border-transparent md:px-0",
            )}
            data-shell-region="context-sidebar"
          >
            <div ref={setSidebarTarget} className="flex min-h-0 flex-col gap-3 md:min-w-56">
              {contextSidebar}
            </div>
          </aside>
        )}
        <main id="main-content" tabIndex={-1} aria-label="Main Workspace" className={cn("min-w-0 flex-1 px-4 py-6 outline-none md:px-8", fitViewport && "md:flex md:min-h-0 md:flex-col md:overflow-hidden md:py-3")} data-shell-region="main-workspace">
          {children}
        </main>
      </div>
      </TopBarTargetContext.Provider>
    </SidebarTargetContext.Provider>
  );
}
