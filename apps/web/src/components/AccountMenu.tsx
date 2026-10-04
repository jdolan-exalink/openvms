import { Link } from "@tanstack/react-router";
import { ChevronDown, KeyRound, LogOut, Moon, Sun } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import { LOCALES, setLocale, useLocale, useT, type Locale } from "@/i18n";
import { cn } from "@/lib/cn";
import { isDarkTheme, useTheme, type ThemeId } from "@/lib/theme";

/** AccountMenu is the compact session menu: language, theme, password, and sign-out. */
export function AccountMenu({
  name,
  username,
  onLogout,
}: {
  name?: string;
  username?: string;
  /** When set, the menu also offers password and sign-out. The login screen omits it. */
  onLogout?: () => void;
}) {
  const t = useT();
  const locale = useLocale();
  const [open, setOpen] = useState(false);
  const [theme, setTheme] = useTheme();
  const light = !isDarkTheme(theme);
  // Remember which dark palette to return to; T2 replaces this toggle with a full selector.
  const lastDark = useRef<ThemeId>("ristretto");
  if (isDarkTheme(theme)) lastDark.current = theme;
  const rootRef = useRef<HTMLDivElement>(null);
  const menuId = useId();
  const signedIn = Boolean(onLogout);
  const initial = (name ?? username ?? "·").slice(0, 1).toUpperCase();

  useEffect(() => {
    if (!open) return;
    const onPointer = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const chooseLocale = (id: Locale) => setLocale(id);
  const chooseTheme = (nextLight: boolean) => setTheme(nextLight ? "light" : lastDark.current);

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={menuId}
        aria-label={signedIn ? t("common.account") : t("common.preferences")}
        onClick={() => setOpen((value) => !value)}
        className={cn(
          "flex h-9 items-center gap-1.5 rounded-full border border-line bg-surface pl-1 pr-2 text-ink shadow-sm",
          "hover:border-accent/50 hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent",
        )}
      >
        <span className="flex size-7 items-center justify-center rounded-full bg-accent/15 text-xs font-semibold text-accent">
          {signedIn ? initial : <Sun className="size-3.5" aria-hidden />}
        </span>
        {signedIn && <span className="hidden max-w-28 truncate text-xs font-medium sm:inline">{name ?? username}</span>}
        <ChevronDown className={cn("size-3.5 text-muted transition-transform", open && "rotate-180")} aria-hidden />
      </button>
      {open && (
        <div
          id={menuId}
          role="menu"
          aria-label={signedIn ? t("common.account") : t("common.preferences")}
          className="absolute right-0 z-50 mt-2 w-60 origin-top-right rounded-2xl border border-line bg-surface/95 p-2 shadow-2xl backdrop-blur-md"
        >
          {signedIn && (
            <div className="mb-2 px-2 pt-1">
              <p className="truncate text-sm font-medium text-ink">{name ?? username}</p>
              {username && name && <p className="truncate text-xs text-muted">{username}</p>}
            </div>
          )}
          <p className="px-2 pb-1 text-[10px] font-medium uppercase tracking-[0.14em] text-muted">{t("common.language")}</p>
          <div role="group" aria-label={t("common.language")} className="mb-2 grid grid-cols-3 gap-1 rounded-xl bg-bg p-1">
            {LOCALES.map((item) => (
              <button
                key={item.id}
                type="button"
                role="menuitemradio"
                aria-checked={item.id === locale}
                onClick={() => chooseLocale(item.id)}
                className={cn(
                  "rounded-lg px-1 py-1.5 text-xs font-medium",
                  item.id === locale ? "bg-accent text-bg shadow-sm" : "text-muted hover:bg-raised hover:text-ink",
                )}
              >
                {item.short}
              </button>
            ))}
          </div>
          <p className="px-2 pb-1 text-[10px] font-medium uppercase tracking-[0.14em] text-muted">{t("common.theme")}</p>
          <div role="group" aria-label={t("common.theme")} className="mb-1 grid grid-cols-2 gap-1 rounded-xl bg-bg p-1">
            <button
              type="button"
              aria-pressed={!light}
              aria-label={t("common.darkMode")}
              onClick={() => chooseTheme(false)}
              className={cn("flex items-center justify-center gap-1.5 rounded-lg px-2 py-1.5 text-xs", !light ? "bg-surface text-ink shadow-sm" : "text-muted hover:text-ink")}
            >
              <Moon className="size-3.5" aria-hidden />
              {t("common.dark")}
            </button>
            <button
              type="button"
              aria-pressed={light}
              aria-label={t("common.lightMode")}
              onClick={() => chooseTheme(true)}
              className={cn("flex items-center justify-center gap-1.5 rounded-lg px-2 py-1.5 text-xs", light ? "bg-surface text-ink shadow-sm" : "text-muted hover:text-ink")}
            >
              <Sun className="size-3.5" aria-hidden />
              {t("common.light")}
            </button>
          </div>
          {signedIn && (
            <>
              <div className="my-1 border-t border-line" />
              <Link
                to="/account"
                role="menuitem"
                onClick={() => setOpen(false)}
                className="flex w-full items-center gap-2 rounded-xl px-2 py-2 text-sm text-ink hover:bg-raised"
              >
                <KeyRound className="size-3.5 text-muted" aria-hidden />
                {t("account.changePassword")}
              </Link>
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setOpen(false);
                  onLogout?.();
                }}
                className="flex w-full items-center gap-2 rounded-xl px-2 py-2 text-left text-sm text-bad hover:bg-bad/10"
              >
                <LogOut className="size-3.5" aria-hidden />
                {t("common.signOut")}
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}
