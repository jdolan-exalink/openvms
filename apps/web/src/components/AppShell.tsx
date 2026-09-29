import type { ReactNode } from "react";

interface AppShellProps {
  primaryNav: ReactNode;
  contextSidebar?: ReactNode;
  children: ReactNode;
}

export function AppShell({ primaryNav, contextSidebar, children }: AppShellProps) {
  return (
    <div className="flex min-h-dvh bg-bg" data-shell="openvms">
      <aside aria-label="Primary Nav Rail" className="hidden w-16 shrink-0 md:flex" data-shell-region="primary-nav">
        {primaryNav}
      </aside>
      {contextSidebar != null && (
        <aside aria-label="Context Sidebar" data-shell-region="context-sidebar">
          {contextSidebar}
        </aside>
      )}
      <main aria-label="Main Workspace" className="min-w-0 flex-1 px-4 py-6 md:px-8" data-shell-region="main-workspace">
        {children}
      </main>
    </div>
  );
}
