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
    <div className="flex flex-col gap-4 md:flex-row md:gap-6">
      <nav
        aria-label={t("nav.settings")}
        className="-mx-4 flex shrink-0 flex-row gap-2 overflow-x-auto px-4 pb-1 md:mx-0 md:w-60 md:flex-col md:gap-4 md:overflow-visible md:px-0 md:pb-0"
      >
        {settingsNavGroups.map((group, i) => (
          <div key={group.title ?? i} className="flex shrink-0 flex-row gap-2 md:flex-col md:gap-1">
            {group.title && (
              <p className="hidden px-4 pb-1 font-mono text-[11px] uppercase tracking-wider text-muted md:block">{t(group.title)}</p>
            )}
            {group.items.filter((item) => !item.permission || can(me.data, item.permission)).map((item) =>
              item.to ? (
                <Link
                  key={item.label}
                  to={item.to}
                  className="m3-press flex h-11 shrink-0 items-center gap-3 rounded-full px-4 text-sm font-medium whitespace-nowrap text-on-surface-variant outline-none hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary aria-[current=page]:bg-primary-container aria-[current=page]:font-bold aria-[current=page]:text-on-primary-container"
                >
                  <Icon icon={item.icon} size="xs" className="shrink-0" />
                  {t(item.label)}
                </Link>
              ) : (
                <span
                  key={item.label}
                  className={cn("flex h-11 shrink-0 cursor-default items-center gap-3 rounded-full px-4 text-sm whitespace-nowrap text-muted/60")}
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
