import { Link } from "@tanstack/react-router";
import { Ellipsis, X } from "lucide-react";
import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import type { Schemas } from "@/api/client";
import { useT } from "@/i18n";
import { cn } from "@/lib/cn";
import type { FeatureFlags } from "@/lib/features";
import { Icon } from "./Icon";
import { bottomNavModel, isNavItemActive, type NavItem } from "./nav";

const FOCUSABLE = 'a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** 64x32 pill indicator holding the icon, with the label underneath (M3 navigation bar item). */
function BarItemBody({ item, active }: { item: Pick<NavItem, "icon">; active: boolean }) {
  return (
    <span
      className={cn(
        "flex h-8 w-16 items-center justify-center rounded-full text-on-surface-variant transition-colors",
        active ? "bg-primary-container text-on-primary-container" : "group-hover:bg-surface-2",
      )}
    >
      <Icon icon={item.icon} size="md" strokeWidth={active ? 2 : undefined} />
    </span>
  );
}

const itemClasses = "group flex min-h-11 flex-1 flex-col items-center justify-center gap-1 rounded-m3-md text-xs outline-none focus-visible:outline-2 focus-visible:outline-primary";

/**
 * Mobile navigation (hidden from md up): a bottom bar with up to four primary destinations and a
 * "More" item that opens a modal sheet with every other permitted destination.
 */
export function MobileNav({ me, features, pathname }: { me: Schemas["Me"] | undefined; features: FeatureFlags; pathname: string }) {
  const t = useT();
  const { primary, more } = bottomNavModel(me, features);
  const [open, setOpen] = useState(false);
  const moreRef = useRef<HTMLButtonElement>(null);
  const sheetRef = useRef<HTMLDivElement>(null);
  const moreActive = more.some((group) => group.items.some((item) => isNavItemActive(item, pathname)));

  const close = () => {
    setOpen(false);
    moreRef.current?.focus();
  };

  useEffect(() => {
    if (!open) return;
    sheetRef.current?.querySelector<HTMLElement>(FOCUSABLE)?.focus();
    const onKeyDown = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false);
        moreRef.current?.focus();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [open]);

  // Keep Tab inside the modal sheet.
  const trapTab = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "Tab") return;
    const nodes = Array.from(sheetRef.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? []);
    const first = nodes[0];
    const last = nodes[nodes.length - 1];
    if (!first || !last) return;
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  };

  return (
    <>
      <div
        data-shell-region="bottom-nav"
        className="fixed inset-x-0 bottom-0 z-40 border-t border-outline-variant bg-surface-1 pb-[env(safe-area-inset-bottom)] md:hidden"
      >
        <nav aria-label={t("common.mobileNav")} className="flex h-20 items-stretch gap-1 px-2 pt-3 pb-4">
          {primary.map((item) => {
            const active = isNavItemActive(item, pathname);
            return (
              <Link key={item.label} to={item.to!} aria-current={active ? "page" : undefined} className={itemClasses}>
                <BarItemBody item={item} active={active} />
                <span className={cn("max-w-full truncate", active ? "font-bold text-on-surface" : "text-on-surface-variant")}>{t(item.label)}</span>
              </Link>
            );
          })}
          <button
            ref={moreRef}
            type="button"
            aria-haspopup="dialog"
            aria-expanded={open}
            onClick={() => setOpen(true)}
            className={itemClasses}
          >
            <BarItemBody item={{ icon: Ellipsis }} active={moreActive} />
            <span className={cn("max-w-full truncate", moreActive ? "font-bold text-on-surface" : "text-on-surface-variant")}>{t("nav.more")}</span>
          </button>
        </nav>
      </div>

      {open && (
        <div data-shell-region="bottom-nav-sheet" className="fixed inset-0 z-50 flex items-end md:hidden">
          <div className="absolute inset-0 bg-scrim/50" aria-hidden onClick={close} />
          <div
            ref={sheetRef}
            role="dialog"
            aria-modal="true"
            aria-label={t("nav.moreTitle")}
            onKeyDown={trapTab}
            className="relative flex max-h-[85dvh] w-full flex-col rounded-t-m3-2xl bg-surface-1 px-4 pt-4 pb-[calc(1rem+env(safe-area-inset-bottom))] shadow-2xl"
          >
            <div className="mb-2 flex items-center justify-between">
              <p className="text-base font-semibold text-on-surface">{t("nav.moreTitle")}</p>
              <button
                type="button"
                onClick={close}
                aria-label={t("common.closeMenu")}
                className="flex size-11 items-center justify-center rounded-full text-on-surface-variant hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-primary"
              >
                <Icon icon={X} />
              </button>
            </div>
            <div className="flex flex-col gap-4 overflow-y-auto">
              {more.map((group, idx) => (
                <div key={group.title ?? idx} className="flex flex-col gap-0.5">
                  {group.title && <p className="px-3 pb-1 text-xs font-medium uppercase tracking-wider text-on-surface-variant">{t(group.title)}</p>}
                  {group.items.map((item) => {
                    const active = isNavItemActive(item, pathname);
                    const row = "flex min-h-12 items-center gap-3 rounded-m3-lg px-3 text-sm";
                    return item.to ? (
                      <Link
                        key={item.label}
                        to={item.to}
                        aria-current={active ? "page" : undefined}
                        onClick={() => setOpen(false)}
                        className={cn(row, "text-on-surface hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-primary", active && "bg-primary-container font-semibold text-on-primary-container hover:bg-primary-container")}
                      >
                        <Icon icon={item.icon} strokeWidth={active ? 2 : undefined} />
                        <span>{t(item.label)}</span>
                      </Link>
                    ) : (
                      <span key={item.label} className={cn(row, "cursor-default text-on-surface-variant/60")} title={t("nav.comingIn", { label: t(item.label), milestone: item.milestone ?? "" })}>
                        <Icon icon={item.icon} />
                        <span>{t(item.label)}</span>
                        <span className="ml-auto font-mono text-[10px]">{item.milestone}</span>
                      </span>
                    );
                  })}
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </>
  );
}
