import { useRef, type KeyboardEvent } from "react";
import { useT, type MessageKey } from "@/i18n";
import { cn } from "@/lib/cn";
import { THEME_IDS, useTheme, type ThemeId } from "@/lib/theme";

/** Preview swatches per theme: [surface, primary, secondary]. Mirrors the palettes in index.css so
 *  each option previews its own theme regardless of the active one. */
const SWATCHES: Record<ThemeId, readonly [string, string, string]> = {
  ristretto: ["#2c2525", "#f38d70", "#85dacc"],
  dracula: ["#282a36", "#bd93f9", "#8be9fd"],
  light: ["#f7f7fa", "#3352c9", "#006a6a"],
};

const LABELS: Record<ThemeId, MessageKey> = {
  ristretto: "common.themeRistretto",
  dracula: "common.themeDracula",
  light: "common.themeLight",
};

/** ThemePicker is an M3 segmented radiogroup that switches the app theme. */
export function ThemePicker({ compact = false, className }: { compact?: boolean; className?: string }) {
  const t = useT();
  const [theme, setTheme] = useTheme();
  const refs = useRef<Partial<Record<ThemeId, HTMLButtonElement | null>>>({});

  const select = (id: ThemeId) => {
    setTheme(id);
    refs.current[id]?.focus();
  };

  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    const last = THEME_IDS.length - 1;
    const next =
      event.key === "ArrowRight" || event.key === "ArrowDown" ? (index + 1) % THEME_IDS.length
      : event.key === "ArrowLeft" || event.key === "ArrowUp" ? (index + last) % THEME_IDS.length
      : event.key === "Home" ? 0
      : event.key === "End" ? last
      : null;
    if (next === null) return;
    event.preventDefault();
    select(THEME_IDS[next]!);
  };

  return (
    <div
      role="radiogroup"
      aria-label={t("common.theme")}
      className={cn("flex w-full gap-1 rounded-full bg-surface-2 p-1", className)}
    >
      {THEME_IDS.map((id, index) => {
        const checked = id === theme;
        return (
          <button
            key={id}
            ref={(node) => {
              refs.current[id] = node;
            }}
            type="button"
            role="radio"
            aria-checked={checked}
            tabIndex={checked ? 0 : -1}
            onClick={() => setTheme(id)}
            onKeyDown={(event) => onKeyDown(event, index)}
            className={cn(
              "flex min-h-11 flex-1 items-center justify-center rounded-full px-2 text-xs font-medium focus-visible:outline-2 focus-visible:outline-primary",
              compact ? "flex-col gap-1 py-1" : "gap-2 px-3 py-2 text-sm",
              checked ? "bg-secondary-container text-on-secondary-container" : "bg-transparent text-muted hover:text-ink",
            )}
          >
            <span aria-hidden className="flex -space-x-1">
              {SWATCHES[id].map((color, i) => (
                <span key={i} className="size-3 rounded-full border border-outline-variant" style={{ backgroundColor: color }} />
              ))}
            </span>
            {t(LABELS[id])}
          </button>
        );
      })}
    </div>
  );
}
