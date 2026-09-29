import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useNavigate } from "@tanstack/react-router";
import { LogOut, Moon, Sun } from "lucide-react";
import { useState } from "react";
import { clearToken } from "@/api/auth";
import { api } from "@/api/client";
import { meQuery } from "@/api/queries";
import { cn } from "@/lib/cn";
import { can } from "@/lib/perm";
import { AppShell } from "./AppShell";
import { brandIcon as Brand, navGroups } from "./nav";

export function Layout() {
  const me = useQuery(meQuery);
  return (
    <AppShell primaryNav={
      <div className="hidden w-60 shrink-0 flex-col border-r border-line bg-surface md:flex">
        <div className="flex items-center gap-2 px-4 py-4">
          <Brand className="size-5 text-accent" aria-hidden />
          <span className="font-semibold tracking-tight">OpenVMS</span>
        </div>
        <nav className="flex flex-1 flex-col gap-4 overflow-y-auto px-2 pb-4" aria-label="Principal">
          {navGroups.map((group, i) => (
            <div key={group.title ?? i} className="flex flex-col gap-0.5">
              {group.title && (
                <p className="px-2 pb-1 font-mono text-[11px] uppercase tracking-wider text-muted">{group.title}</p>
              )}
              {group.items.filter((item) => !item.permission || can(me.data, item.permission)).map((item) =>
                item.to ? (
                  <Link
                    key={item.label}
                    to={item.to}
                    className="flex items-center gap-2 rounded px-2 py-1.5 text-sm text-muted hover:bg-raised hover:text-ink"
                    activeProps={{ className: "bg-raised !text-ink" }}
                  >
                    <item.icon className="size-4" aria-hidden />
                    {item.label}
                  </Link>
                ) : (
                  <span
                    key={item.label}
                    className="flex cursor-default items-center gap-2 rounded px-2 py-1.5 text-sm text-muted/60"
                    title={`Llega en ${item.milestone}`}
                  >
                    <item.icon className="size-4" aria-hidden />
                    {item.label}
                    <span className="ml-auto font-mono text-[10px]">{item.milestone}</span>
                  </span>
                ),
              )}
            </div>
          ))}
        </nav>
        <div className="flex flex-col gap-0.5 border-t border-line p-2">
          <UserBox />
          <ThemeToggle />
        </div>
      </div>
    }>
      <Outlet />
    </AppShell>
  );
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
    <div className="flex items-center gap-2 px-2 py-1.5 text-sm">
      <span className="min-w-0 flex-1 truncate" title={me.data?.username}>
        {me.data?.display_name ?? "…"}
        {me.data && me.data.tenant_id === null && <span className="ml-1.5 font-mono text-[10px] text-muted">PLATAFORMA</span>}
      </span>
      <button
        type="button"
        onClick={() => void logout()}
        className="rounded p-1 text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
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
      className={cn(
        "flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-muted hover:bg-raised hover:text-ink",
        "focus-visible:outline-2 focus-visible:outline-accent",
      )}
    >
      {light ? <Moon className="size-4" aria-hidden /> : <Sun className="size-4" aria-hidden />}
      {light ? "Modo oscuro" : "Modo claro"}
    </button>
  );
}
