import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { LogOut, Menu, Moon, Search, Sun, X } from "lucide-react";
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
import { AppShell, TopBarActionsSlot } from "./AppShell";
import { brandIcon as Brand, navGroups, settingsNavGroups, type NavGroup } from "./nav";
import { NotificationBell } from "./NotificationBell";
import { Omnibox } from "./Omnibox";

export function Layout() {
  const me = useQuery(meQuery);
  useRealtimeFeed(); // one app-wide push feed; Layout only renders for authenticated routes
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const search = useRouterState({ select: (state) => state.location.search }) as Record<string, unknown>;
  const isLive = pathname === "/live";
  const liveRec = isLive && parseRecSearch(search).rec && can(me.data, "recordings.view");
  const pageContext = getPageContext(pathname);
  const pageTitle = isLive && liveRec ? "Grabación" : pageContext.title;
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
            <div className="hidden w-16 shrink-0 flex-col items-center border-r border-line bg-surface py-3 md:flex">
              <Link to="/live" aria-label="OpenVMS: En vivo" title="OpenVMS" className="mb-5 flex size-10 items-center justify-center rounded-xl text-accent hover:bg-raised">
                <Brand className="size-5" aria-hidden />
              </Link>
              <nav className="flex w-full flex-1 flex-col items-center gap-4 overflow-y-auto pb-3" aria-label="Navegación principal">
                {navGroups.map((group, i) => (
                  <NavGroupLinks key={group.title ?? i} group={group} me={me.data} pathname={pathname} />
                ))}
              </nav>
              <div className="flex w-full flex-col items-center gap-1 border-t border-line pt-3">
                <UserBox />
                <ThemeToggle />
              </div>
            </div>
          }
          fitViewport={isLive}
          contextSidebar={isLive ? <div id="live-context-sidebar" className="flex min-h-0 flex-col gap-3" /> : undefined}
        >
          <div className={cn("min-w-0", isLive && "md:flex md:min-h-0 md:flex-1 md:flex-col")}>
            <header className={cn("flex min-h-14 items-center justify-between gap-3 border-b border-line", isLive ? "mb-2 pb-2 md:min-h-11 md:shrink-0" : "mb-6 pb-4")} aria-label="Encabezado de página">
              <div className="flex min-w-0 items-center gap-3">
                <button
                  type="button"
                  onClick={() => setMobileOpen(true)}
                  aria-label="Abrir menú de navegación"
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
                <NotificationBell />
                <button
                  type="button"
                  onClick={() => setOmniboxOpen(true)}
                  aria-label="Buscar en OpenVMS (Ctrl+K)"
                  className="flex items-center gap-2 rounded-lg border border-line bg-surface px-3 py-1.5 text-xs text-muted hover:bg-raised hover:text-ink transition-colors"
                >
                  <Search className="size-3.5" aria-hidden />
                  <span className="hidden sm:inline">Buscar...</span>
                  <kbd className="hidden sm:inline-flex items-center gap-0.5 rounded border border-line bg-bg px-1.5 py-0.5 font-mono text-[10px] text-muted">
                    <span className="text-xs">⌘</span>K
                  </kbd>
                </button>
              </div>
            </header>
            {isLive ? (
              <div className="md:min-h-0 md:flex-1">
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
                  aria-label="Cerrar menú"
                  className="flex size-8 items-center justify-center rounded-lg text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
                >
                  <X className="size-4" aria-hidden />
                </button>
              </div>
              <nav className="flex flex-1 flex-col gap-4 overflow-y-auto pr-1" aria-label="Navegación móvil">
                {mobileNavGroups.map((group, idx) => (
                  <div key={group.title ?? idx} className="flex flex-col gap-0.5">
                    {group.title && (
                      <p className="px-2 pb-1 font-mono text-[10px] uppercase tracking-wider text-muted">{group.title}</p>
                    )}
                    {group.items.filter((item) => !item.permission || can(me.data, item.permission)).map((item) => {
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
                          <item.icon className="size-4 shrink-0" aria-hidden />
                          <span>{item.label}</span>
                        </Link>
                      ) : (
                        <span
                          key={item.label}
                          className="flex cursor-default items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm text-muted/50"
                          title={`Llega en ${item.milestone}`}
                        >
                          <item.icon className="size-4 shrink-0" aria-hidden />
                          <span>{item.label}</span>
                          <span className="ml-auto font-mono text-[10px]">{item.milestone}</span>
                        </span>
                      );
                    })}
                  </div>
                ))}
              </nav>
              <div className="mt-auto flex items-center justify-between border-t border-line pt-3">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-raised text-xs font-semibold text-ink">
                    {me.data?.display_name?.slice(0, 1).toUpperCase() ?? "…"}
                  </span>
                  <span className="truncate text-xs text-muted" title={me.data?.username}>
                    {me.data?.display_name ?? me.data?.username}
                  </span>
                </div>
                <div className="flex items-center gap-1">
                  <ThemeToggle />
                  <button
                    type="button"
                    onClick={() => {
                      setMobileOpen(false);
                      void logout();
                    }}
                    className="flex size-9 items-center justify-center rounded-lg text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
                    aria-label="Cerrar sesión"
                    title="Cerrar sesión"
                  >
                    <LogOut className="size-4" aria-hidden />
                  </button>
                </div>
              </div>
            </div>
          </div>
        )}
      </VideoSurfaceLayer>
    </PlayerSessionProvider>
  );
}

function NavGroupLinks({ group, me, pathname }: { group: NavGroup; me: Schemas["Me"] | undefined; pathname: string }) {
  return (
    <div className="flex flex-col items-center gap-1">
      {group.items.filter((item) => !item.permission || can(me, item.permission)).map((item) => {
        const settingsRouteActive = settingsNavGroups.some((settingsGroup) => settingsGroup.items.some((settingsItem) => settingsItem.to && (pathname === settingsItem.to || pathname.startsWith(`${settingsItem.to}/`))));
        const active = item.to != null && (pathname === item.to || (item.to === "/settings" && settingsRouteActive) || (item.to !== "/settings" && item.to !== "/live" && pathname.startsWith(`${item.to}/`)));
        const classes = cn(
          "flex size-10 items-center justify-center rounded-xl text-muted transition-colors hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent",
          active && "bg-accent/15 text-accent ring-1 ring-inset ring-accent/30 hover:bg-accent/20 hover:text-accent",
          !item.to && "cursor-default text-muted/50",
        );
        return item.to ? (
          <Link key={item.label} to={item.to} aria-label={item.label} aria-current={active ? "page" : undefined} title={item.label} className={classes} activeProps={{ "aria-current": "page" }}>
            <item.icon className="size-[18px]" aria-hidden />
          </Link>
        ) : (
          <span key={item.label} aria-label={`${item.label}, próximamente`} title={`${item.label} · Llega en ${item.milestone}`} className={classes}>
            <item.icon className="size-[18px]" aria-hidden />
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
    if (item) return { section: group.title ?? (item.to === "/settings" ? "Configuración" : item.to === "/live" ? "Operaciones" : "OpenVMS"), title: item.label };
  }
  return { section: "OpenVMS", title: "Workspace" };
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

function UserBox() {
  const me = useQuery(meQuery);
  const logout = useLogout();
  return (
    <div className="flex flex-col items-center gap-1">
      <span className="flex size-8 items-center justify-center rounded-full bg-raised text-xs font-semibold text-ink" title={me.data?.username} aria-label={me.data?.display_name ?? "Cuenta"}>
        {me.data?.display_name?.slice(0, 1).toUpperCase() ?? "…"}
      </span>
      <button
        type="button"
        onClick={() => void logout()}
        className="flex size-9 items-center justify-center rounded-lg text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
        aria-label="Cerrar sesión"
        title="Cerrar sesión"
      >
        <LogOut className="size-4" aria-hidden />
      </button>
    </div>
  );
}

function ThemeToggle() {
  const [light, setLight] = useState(() => document.documentElement.classList.contains("light"));
  const toggle = () => {
    const next = !light;
    document.documentElement.classList.toggle("light", next);
    document.documentElement.classList.toggle("dark", !next);
    try {
      localStorage.setItem("openvms.theme", next ? "light" : "dark");
    } catch {
      // storage can be unavailable (private mode); the toggle still works for this session
    }
    setLight(next);
  };
  return (
    <button
      type="button"
      onClick={toggle}
      aria-label={light ? "Modo oscuro" : "Modo claro"}
      title={light ? "Modo oscuro" : "Modo claro"}
      className={cn(
        "flex size-9 items-center justify-center rounded-lg text-muted hover:bg-raised hover:text-ink",
        "focus-visible:outline-2 focus-visible:outline-accent",
      )}
    >
      {light ? <Moon className="size-4" aria-hidden /> : <Sun className="size-4" aria-hidden />}
    </button>
  );
}
