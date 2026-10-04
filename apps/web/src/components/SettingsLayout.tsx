import { useQuery } from "@tanstack/react-query";
import { Link, Outlet } from "@tanstack/react-router";
import { meQuery } from "@/api/queries";
import { useT } from "@/i18n";
import { can } from "@/lib/perm";
import { cn } from "@/lib/cn";
import { Icon } from "./Icon";
import { settingsNavGroups } from "./nav";

/**
 * SettingsLayout wraps the infrastructure/administration pages that used to live as separate
 * top-level sidebar sections. It renders its own sub-navigation and an Outlet for the active
 * settings page; the pages themselves keep their existing routes and permission checks.
 */
export function SettingsLayout() {
  const t = useT();
  const me = useQuery(meQuery);
  return (
    <div className="flex flex-col gap-6 lg:flex-row">
        <nav aria-label={t("nav.settings")} className="flex w-full shrink-0 flex-col gap-4 lg:w-56">
          {settingsNavGroups.map((group, i) => (
            <div key={group.title ?? i} className="flex flex-col gap-0.5">
              {group.title && (
                <p className="px-2 pb-1 font-mono text-[11px] uppercase tracking-wider text-muted">{t(group.title)}</p>
              )}
              {group.items.filter((item) => !item.permission || can(me.data, item.permission)).map((item) =>
                item.to ? (
                  <Link
                    key={item.label}
                    to={item.to}
                    className="flex items-center gap-2 rounded px-2 py-1.5 text-sm text-muted hover:bg-raised hover:text-ink"
                    activeProps={{ className: "bg-raised !text-ink" }}
                  >
                    <Icon icon={item.icon} size="xs" className="shrink-0" />
                    {t(item.label)}
                  </Link>
                ) : (
                  <span
                    key={item.label}
                    className={cn("flex cursor-default items-center gap-2 rounded px-2 py-1.5 text-sm text-muted/60")}
                    title={t("nav.comingIn", { label: t(item.label), milestone: item.milestone ?? "" })}
                  >
                    <Icon icon={item.icon} size="xs" className="shrink-0" />
                    {t(item.label)}
                    <span className="ml-auto font-mono text-[10px]">{item.milestone}</span>
                  </span>
                ),
              )}
            </div>
          ))}
        </nav>
        <div className="min-w-0 flex-1">
          <Outlet />
        </div>
      </div>
  );
}
