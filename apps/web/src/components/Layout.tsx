import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { clearToken } from "@/api/auth";
import { api, type Schemas } from "@/api/client";
import { meQuery } from "@/api/queries";
import { cn } from "@/lib/cn";
import { can } from "@/lib/perm";
import { parseRecSearch } from "@/lib/liveRec";
import { PlayerSessionProvider } from "@/lib/live/PlayerSessionProvider";
import { VideoSurfaceLayer } from "@/lib/live/SurfaceLayer";
import { useRealtimeFeed } from "@/lib/realtime";
import { useFeatures, type FeatureFlags } from "@/lib/features";
import { useT } from "@/i18n";
import { AgentJobProvider } from "@/lib/agentJobs/AgentJobProvider";
import { AgentJobStatusRegion } from "./AgentJobStatusRegion";
import { useInstallSheet } from "@/lib/pwa/useInstallSheet";
import { AppShell, TopBarActionsSlot } from "./AppShell";
import { AccountMenu } from "./AccountMenu";
import { InstallSheet } from "./InstallSheet";
import { PwaUpdatePrompt } from "./PwaUpdatePrompt";
import { Icon } from "./Icon";
import { MobileNav } from "./MobileNav";
import { brandIcon as Brand, isNavItemActive, isNavItemVisible, navGroups, settingsNavGroups, type NavGroup } from "./nav";
import { NotificationBell } from "./NotificationBell";
import { Omnibox } from "./Omnibox";

export function Layout() {
  const me = useQuery(meQuery);
  const features = useFeatures();
  useRealtimeFeed(); // one app-wide push feed; Layout only renders for authenticated routes
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const search = useRouterState({ select: (state) => state.location.search }) as Record<string, unknown>;
  const isLive = pathname === "/live";
  const isMaps = pathname === "/maps";
  const fitWorkspace = isLive || isMaps;
  const liveRec = isLive && parseRecSearch(search).rec && can(me.data, "recordings.view");
  const t = useT();
  const pageContextRaw = getPageContext(pathname);
  const pageContext = { section: t(pageContextRaw.section), title: t(pageContextRaw.title) };
  const pageTitle = isLive && liveRec ? t("nav.recordings") : pageContext.title;
  const [omniboxOpen, setOmniboxOpen] = useState(false);
  const logout = useLogout();
  const install = useInstallSheet();

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOmniboxOpen((prev) => !prev);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  return (
    <AgentJobProvider key={me.data?.id ?? "pending-session"}>
    <PlayerSessionProvider userId={me.data?.id}>
      <VideoSurfaceLayer>
        <AppShell
          primaryNav={
            <div className="flex min-h-full w-(--rail-w) shrink-0 flex-col items-center bg-surface-dim py-3">
              <Link to="/live" aria-label={t("common.brandLive")} title="OpenVMS" className="mb-5 flex size-12 shrink-0 items-center justify-center rounded-m3-lg bg-primary text-on-primary outline-none transition-opacity hover:opacity-90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">
                <Brand className="size-6" aria-hidden />
              </Link>
              <nav className="flex w-full flex-1 flex-col items-center gap-3 overflow-y-auto" aria-label={t("common.mainNav")}>
                {navGroups.map((group, i) => (
                  <NavGroupLinks key={group.title ?? i} group={group} me={me.data} pathname={pathname} features={features} />
                ))}
              </nav>
            </div>
          }
          bottomNav={<MobileNav me={me.data} features={features} pathname={pathname} />}
          fitViewport={fitWorkspace}
          contextSidebar={isLive ? <div id="live-context-sidebar" className="flex min-h-0 flex-col gap-3" /> : undefined}
        >
          <div className={cn("min-w-0", fitWorkspace && "flex min-h-0 flex-1 flex-col")}>
            <header data-shell-region="page-header" className={cn("flex min-h-14 items-center justify-between gap-3 border-b border-line", fitWorkspace ? "mb-2 shrink-0 pb-2 md:min-h-11" : "mb-6 pb-4")} aria-label={t("common.pageHeader")}>
              <div className="flex min-w-0 items-center gap-3">
                {/* On /live the mode toggle replaces the breadcrumb to save space; the title stays for screen readers. */}
                <div className={cn("min-w-0", isLive && "sr-only")}>
                  <p className="truncate text-lg font-semibold tracking-tight text-ink">
                    <span className="font-mono text-[10px] uppercase tracking-[0.16em] text-muted">{pageContext.section}</span>
                    <span className="ml-2">{`/ ${pageTitle}`}</span>
                  </p>
                </div>
                {isLive && <TopBarActionsSlot className="flex shrink-0 items-center" />}
              </div>
              <div className="flex items-center gap-3">
                {/* Notifications are tenant-scoped; the tenant-less platform admin would only collect 403s. */}
                {me.data?.tenant_id != null && <NotificationBell />}
                <button
                  type="button"
                  onClick={() => setOmniboxOpen(true)}
                  aria-label={t("common.searchOpenVms")}
                  className="flex items-center gap-2 rounded-lg border border-line bg-surface px-3 py-1.5 text-xs text-muted hover:bg-raised hover:text-ink transition-colors"
                >
                  <Search className="size-3.5" aria-hidden />
                  <span className="hidden sm:inline">{t("common.searchPlaceholder")}</span>
                  <kbd className="hidden sm:inline-flex items-center gap-0.5 rounded border border-line bg-bg px-1.5 py-0.5 font-mono text-[10px] text-muted">
                    <span className="text-xs">⌘</span>K
                  </kbd>
                </button>
                <AccountMenu name={me.data?.display_name} username={me.data?.username} onLogout={() => void logout()} onInstall={install.show} />
              </div>
            </header>
            <AgentJobStatusRegion />
            {fitWorkspace ? (
              <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
                <Outlet />
              </div>
            ) : (
              <Outlet />
            )}
          </div>
        </AppShell>
  
        <Omnibox isOpen={omniboxOpen} onClose={() => setOmniboxOpen(false)} />
        <InstallSheet open={install.open} onClose={install.dismiss} />
        <PwaUpdatePrompt />
      </VideoSurfaceLayer>
    </PlayerSessionProvider>
    </AgentJobProvider>
  );
}

/**
 * M3 plain tooltip for icon-only rail items. It is position: fixed because the rail scrolls
 * (overflow clips absolute children); it opens on hover after a short delay and immediately on
 * keyboard focus, and Escape dismisses it.
 */
function RailTooltipTarget({ label, children }: { label: string; children: (props: { ref: (el: HTMLElement | null) => void; describedBy: string | undefined; handlers: Record<string, unknown> }) => ReactNode }) {
  const id = useId();
  const el = useRef<HTMLElement | null>(null);
  const timer = useRef<number | undefined>(undefined);
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null);
  const show = () => {
    const r = el.current?.getBoundingClientRect();
    if (r) setPos({ top: r.top + r.height / 2, left: r.right + 8 });
  };
  const hide = () => {
    window.clearTimeout(timer.current);
    setPos(null);
  };
  useEffect(() => () => window.clearTimeout(timer.current), []);
  // Touch has no hover: the emulated enter and the focus a tap gives would leave the tooltip stuck.
  const touch = useRef(false);
  const handlers = {
    onPointerEnter: (e: { pointerType: string }) => {
      window.clearTimeout(timer.current);
      if (e.pointerType === "touch") return;
      timer.current = window.setTimeout(show, 400);
    },
    onPointerDown: (e: { pointerType: string }) => {
      touch.current = e.pointerType === "touch";
      if (touch.current) hide();
    },
    onPointerLeave: hide,
    onFocus: () => { if (!touch.current) show(); },
    onBlur: hide,
    onKeyDown: (e: { key: string }) => {
      touch.current = false;
      if (e.key === "Escape") hide();
    },
  };
  return (
    <>
      {children({ ref: (node) => { el.current = node; }, describedBy: pos ? id : undefined, handlers })}
      {pos && (
        <span id={id} role="tooltip" style={{ top: pos.top, left: pos.left }} className="pointer-events-none fixed z-50 -translate-y-1/2 whitespace-nowrap rounded-m3-sm bg-surface-3 px-2 py-1 text-xs font-medium text-on-surface shadow-md animate-in fade-in duration-100 motion-reduce:animate-none">
          {label}
        </span>
      )}
    </>
  );
}

function NavGroupLinks({ group, me, pathname, features }: { group: NavGroup; me: Schemas["Me"] | undefined; pathname: string; features: FeatureFlags }) {
  const t = useT();
  return (
    <div className="flex flex-col items-center gap-3">
      {group.items.filter((item) => isNavItemVisible(item, me, features)).map((item) => {
        const active = isNavItemActive(item, pathname);
        const label = t(item.label);
        const body = <Icon icon={item.icon} size="md" strokeWidth={active ? 2 : undefined} />;
        const classes = cn(
          "group flex size-12 shrink-0 items-center justify-center rounded-full outline-none transition-colors focus-visible:outline-2 focus-visible:outline-primary",
          active ? "bg-primary-container text-on-primary-container" : "text-on-surface-variant hover:bg-surface-2",
          !item.to && "cursor-default opacity-50",
        );
        return (
          <RailTooltipTarget key={item.label} label={item.to ? label : t("nav.comingIn", { label, milestone: item.milestone ?? "" })}>
            {({ ref, describedBy, handlers }) => item.to ? (
              <Link ref={ref} to={item.to} aria-label={label} aria-describedby={describedBy} aria-current={active ? "page" : undefined} className={classes} {...handlers}>
                {body}
              </Link>
            ) : (
              <span ref={ref} tabIndex={0} aria-label={t("nav.comingSoon", { label })} aria-describedby={describedBy} className={classes} {...handlers}>
                {body}
              </span>
            )}
          </RailTooltipTarget>
        );
      })}
    </div>
  );
}

function getPageContext(pathname: string) {
  const groups = [...settingsNavGroups, ...navGroups];
  for (const group of groups) {
    const item = group.items.find((candidate) => candidate.to && (pathname === candidate.to || (candidate.to !== "/live" && candidate.to !== "/settings" && pathname.startsWith(`${candidate.to}/`))));
    if (item) return { section: group.title ?? (item.to === "/settings" ? "nav.settings" : item.to === "/live" ? "nav.operations" : "nav.brand"), title: item.label };
  }
  return { section: "nav.brand" as const, title: "nav.workspace" as const };
}

function useLogout() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  return async () => {
    try {
      await api.POST("/api/v1/auth/logout");
    } catch {
      // the session may already be gone; clear local state anyway
    }
    clearToken();
    qc.clear();
    void navigate({ to: "/login" });
  };
}
