import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { Menu, Search, X } from "lucide-react";
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
import { brandIcon as Brand, navGroups, settingsNavGroups, type NavGroup } from "./nav";
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
  const [mobileOpen, setMobileOpen] = useState(false);
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

  useEffect(() => {
    if (!mobileOpen) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMobileOpen(false);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [mobileOpen]);

  const mobileNavGroups: NavGroup[] = [
    ...navGroups.filter((g) => !g.items.some((i) => i.to === "/settings")),
    ...settingsNavGroups,
  ];

  return (
    <PlayerSessionProvider userId={me.data?.id}>
      <VideoSurfaceLayer>
        <AppShell
          primaryNav={
            <div className="flex min-h-full w-16 shrink-0 flex-col items-center border-r border-line bg-surface py-3">
              <Link to="/live" aria-label={t("common.brandLive")} title="OpenVMS" className="mb-5 flex size-10 items-center justify-center rounded-xl text-accent hover:bg-raised">
                <Brand className="size-5" aria-hidden />
              </Link>
              <nav className="flex w-full flex-1 flex-col items-center gap-4 overflow-y-auto" aria-label={t("common.mainNav")}>
                {navGroups.map((group, i) => (
                  <NavGroupLinks key={group.title ?? i} group={group} me={me.data} pathname={pathname} features={features} />
                ))}
              </nav>
            </div>
          }
          fitViewport={fitWorkspace}
          contextSidebar={isLive ? <div id="live-context-sidebar" className="flex min-h-0 flex-col gap-3" /> : undefined}
        >
          <div className={cn("min-w-0", fitWorkspace && "flex min-h-0 flex-1 flex-col")}>
            <header data-shell-region="page-header" className={cn("flex min-h-14 items-center justify-between gap-3 border-b border-line", fitWorkspace ? "mb-2 shrink-0 pb-2 md:min-h-11" : "mb-6 pb-4")} aria-label={t("common.pageHeader")}>
              <div className="flex min-w-0 items-center gap-3">
                <button
                  type="button"
                  onClick={() => setMobileOpen(true)}
                  aria-label={t("common.openMenu")}
                  aria-expanded={mobileOpen}
                  aria-controls="mobile-nav-drawer"
                  className="flex size-9 items-center justify-center rounded-lg text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent md:hidden"
                >
                  <Menu className="size-5" aria-hidden />
                </button>
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
  
        {mobileOpen && (
          <div
            role="dialog"
            aria-modal="true"
            aria-label="Navegación móvil"
            id="mobile-nav-drawer"
            className="fixed inset-0 z-50 flex md:hidden"
          >
            <div
              className="fixed inset-0 bg-black/60 backdrop-blur-xs transition-opacity"
              aria-hidden
              onClick={() => setMobileOpen(false)}
            />
            <div className="relative flex w-80 max-w-[85vw] flex-col border-r border-line bg-surface p-4 shadow-2xl">
              <div className="mb-4 flex items-center justify-between border-b border-line pb-3">
                <Link
                  to="/live"
                  onClick={() => setMobileOpen(false)}
                  className="flex items-center gap-2 font-semibold text-ink hover:text-accent"
                >
                  <Brand className="size-5 text-accent" aria-hidden />
                  <span>OpenVMS</span>
                </Link>
                <button
                  type="button"
                  onClick={() => setMobileOpen(false)}
                  aria-label={t("common.closeMenu")}
                  className="flex size-8 items-center justify-center rounded-lg text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
                >
                  <X className="size-4" aria-hidden />
                </button>
              </div>
              <nav className="flex flex-1 flex-col gap-4 overflow-y-auto pr-1" aria-label={t("common.mobileNav")}>
                {mobileNavGroups.map((group, idx) => (
                  <div key={group.title ?? idx} className="flex flex-col gap-0.5">
                    {group.title && (
                      <p className="px-2 pb-1 font-mono text-[10px] uppercase tracking-wider text-muted">{t(group.title)}</p>
                    )}
                    {group.items.filter((item) => (!item.permission || can(me.data, item.permission)) && (!item.feature || features[item.feature])).map((item) => {
                      const active = item.to != null && (pathname === item.to || (item.to !== "/live" && item.to !== "/settings" && pathname.startsWith(`${item.to}/`)));
                      return item.to ? (
                        <Link
                          key={item.label}
                          to={item.to}
                          onClick={() => setMobileOpen(false)}
                          className={cn(
                            "flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm text-muted transition-colors hover:bg-raised hover:text-ink",
                            active && "bg-accent/15 font-medium text-accent ring-1 ring-inset ring-accent/30",
                          )}
                          activeProps={{ "aria-current": "page" }}
                        >
                          <Icon icon={item.icon} size="xs" className="shrink-0" strokeWidth={active ? 2 : undefined} />
                          <span>{t(item.label)}</span>
                        </Link>
                      ) : (
                        <span
                          key={item.label}
                          className="flex cursor-default items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm text-muted/50"
                          title={t("nav.comingIn", { label: t(item.label), milestone: item.milestone ?? "" })}
                        >
                          <Icon icon={item.icon} size="xs" className="shrink-0" strokeWidth={active ? 2 : undefined} />
                          <span>{t(item.label)}</span>
                          <span className="ml-auto font-mono text-[10px]">{item.milestone}</span>
                        </span>
                      );
                    })}
                  </div>
                ))}
              </nav>
            </div>
          </div>
        )}
      </VideoSurfaceLayer>
    </PlayerSessionProvider>
  );
}

function NavGroupLinks({ group, me, pathname, features }: { group: NavGroup; me: Schemas["Me"] | undefined; pathname: string; features: FeatureFlags }) {
  const t = useT();
  return (
    <div className="flex flex-col items-center gap-1">
      {group.items.filter((item) => (!item.permission || can(me, item.permission)) && (!item.feature || features[item.feature])).map((item) => {
        const settingsRouteActive = settingsNavGroups.some((settingsGroup) => settingsGroup.items.some((settingsItem) => settingsItem.to && (pathname === settingsItem.to || pathname.startsWith(`${settingsItem.to}/`))));
        const active = item.to != null && (pathname === item.to || (item.to === "/settings" && settingsRouteActive) || (item.to !== "/settings" && item.to !== "/live" && pathname.startsWith(`${item.to}/`)));
        const classes = cn(
          "flex size-10 items-center justify-center rounded-xl text-muted transition-colors hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent",
          active && "bg-accent/15 text-accent ring-1 ring-inset ring-accent/30 hover:bg-accent/20 hover:text-accent",
          !item.to && "cursor-default text-muted/50",
        );
        return item.to ? (
          <Link key={item.label} to={item.to} aria-label={t(item.label)} aria-current={active ? "page" : undefined} title={t(item.label)} className={classes} activeProps={{ "aria-current": "page" }}>
            <Icon icon={item.icon} strokeWidth={active ? 2 : undefined} />
          </Link>
        ) : (
          <span key={item.label} aria-label={t("nav.comingSoon", { label: t(item.label) })} title={t("nav.comingIn", { label: t(item.label), milestone: item.milestone ?? "" })} className={classes}>
            <Icon icon={item.icon} strokeWidth={active ? 2 : undefined} />
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

