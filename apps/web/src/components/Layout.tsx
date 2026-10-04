import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { useEffect, useState } from "react";
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
import { AppShell, TopBarActionsSlot } from "./AppShell";
import { AccountMenu } from "./AccountMenu";
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
    <PlayerSessionProvider userId={me.data?.id}>
      <VideoSurfaceLayer>
        <AppShell
          primaryNav={
            <div className="flex min-h-full w-22 shrink-0 flex-col items-center bg-surface-dim py-3">
              <Link to="/live" aria-label={t("common.brandLive")} title="OpenVMS" className="mb-5 flex size-12 items-center justify-center rounded-m3-lg bg-primary text-on-primary outline-none transition-opacity hover:opacity-90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">
                <Brand className="size-6" aria-hidden />
              </Link>
              <nav className="flex w-full flex-1 flex-col items-center gap-4 overflow-y-auto" aria-label={t("common.mainNav")}>
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
                <AccountMenu name={me.data?.display_name} username={me.data?.username} onLogout={() => void logout()} />
              </div>
            </header>
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
      </VideoSurfaceLayer>
    </PlayerSessionProvider>
  );
}

function NavGroupLinks({ group, me, pathname, features }: { group: NavGroup; me: Schemas["Me"] | undefined; pathname: string; features: FeatureFlags }) {
  const t = useT();
  return (
    <div className="flex flex-col items-center gap-3">
      {group.items.filter((item) => isNavItemVisible(item, me, features)).map((item) => {
        const active = isNavItemActive(item, pathname);
        const body = (
          <>
            <span className={cn("flex h-8 w-14 items-center justify-center rounded-full transition-colors", active ? "bg-primary-container text-on-primary-container" : "group-hover:bg-surface-2")}>
              <Icon icon={item.icon} size="md" strokeWidth={active ? 2 : undefined} />
            </span>
            <span className={cn("max-w-full truncate px-1 text-xs", active ? "font-bold text-on-surface" : "text-on-surface-variant")}>{t(item.label)}</span>
          </>
        );
        const classes = cn(
          "group flex w-20 min-h-14 flex-col items-center gap-1 rounded-m3-md outline-none focus-visible:outline-2 focus-visible:outline-primary",
          !item.to && "cursor-default opacity-50",
        );
        return item.to ? (
          <Link key={item.label} to={item.to} aria-current={active ? "page" : undefined} className={classes}>
            {body}
          </Link>
        ) : (
          <span key={item.label} aria-label={t("nav.comingSoon", { label: t(item.label) })} title={t("nav.comingIn", { label: t(item.label), milestone: item.milestone ?? "" })} className={classes}>
            {body}
          </span>
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

