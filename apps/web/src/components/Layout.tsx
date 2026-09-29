import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { LogOut, Moon, Sun } from "lucide-react";
import { useState } from "react";
import { clearToken } from "@/api/auth";
import { api, type Schemas } from "@/api/client";
import { meQuery } from "@/api/queries";
import { cn } from "@/lib/cn";
import { can } from "@/lib/perm";
import { useRealtimeFeed } from "@/lib/realtime";
import { AppShell } from "./AppShell";
import { brandIcon as Brand, navGroups, settingsNavGroups, type NavGroup } from "./nav";

export function Layout() {
  const me = useQuery(meQuery);
  useRealtimeFeed(); // one app-wide push feed; Layout only renders for authenticated routes
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const pageContext = getPageContext(pathname);
  return (
    <AppShell primaryNav={
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
    } contextSidebar={pathname === "/live" ? <div id="live-context-sidebar" className="flex min-h-0 flex-col gap-3" /> : undefined}>
      <div className="min-w-0">
        <header className="mb-6 flex min-h-14 items-center justify-between border-b border-line pb-4" aria-label="Encabezado de página">
          <div className="min-w-0">
            <p className="truncate text-lg font-semibold tracking-tight text-ink">
              <span className="font-mono text-[10px] uppercase tracking-[0.16em] text-muted">{pageContext.section}</span>
              <span className="ml-2">{`/ ${pageContext.title}`}</span>
            </p>
          </div>
        </header>
        <Outlet />
      </div>
    </AppShell>
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

function UserBox() {
  const me = useQuery(meQuery);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const logout = async () => {
    try {
      await api.POST("/api/v1/auth/logout");
    } catch {
      // the session may already be gone; clear local state anyway
    }
    clearToken();
    qc.clear();
    void navigate({ to: "/login" });
  };
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
