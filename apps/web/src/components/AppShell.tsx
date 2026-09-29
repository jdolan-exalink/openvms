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

interface AppShellProps {
  primaryNav: ReactNode;
  contextSidebar?: ReactNode;
  children: ReactNode;
}

export function AppShell({ primaryNav, contextSidebar, children }: AppShellProps) {
  return (
    <SidebarTargetContext.Provider value={contextSidebar != null}>
      <div className="flex min-h-dvh flex-col bg-bg md:flex-row" data-shell="openvms">
        <aside aria-label="Primary Nav Rail" className="hidden w-16 shrink-0 md:flex" data-shell-region="primary-nav">
          {primaryNav}
        </aside>
        {contextSidebar != null && (
          <aside aria-label="Context Sidebar" className="w-full shrink-0 border-b border-line bg-surface px-3 py-4 md:w-64 md:border-b-0 md:border-r" data-shell-region="context-sidebar">
            <div ref={setSidebarTarget} className="flex min-h-0 flex-col gap-3">
              {contextSidebar}
            </div>
          </aside>
        )}
        <main aria-label="Main Workspace" className="min-w-0 flex-1 px-4 py-6 md:px-8" data-shell-region="main-workspace">
          {children}
        </main>
      </div>
    </SidebarTargetContext.Provider>
  );
}
