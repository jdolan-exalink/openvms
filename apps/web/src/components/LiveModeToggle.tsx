import { Circle, Play } from "lucide-react";
import { useT } from "@/i18n";
import { cn } from "@/lib/cn";

/**
 * LiveModeToggle is the LIVE/REC segmented control shown in the top bar of the Live screen.
 * VIVO carries a green accent with a pulsing dot when active; GRABADO carries a red one.
 */
export function LiveModeToggle({ rec, onChange }: { rec: boolean; onChange: (mode: "live" | "rec") => void }) {
  const t = useT();
  const options = [
    { mode: "live", label: "Vivo", Icon: Play, hint: "Ver las cámaras en vivo", active: "bg-ok/15 text-ok ring-ok/40", dot: "bg-ok" },
    { mode: "rec", label: "Grabado", Icon: Circle, hint: "Reproducir desde cinco minutos antes", active: "bg-bad/15 text-bad ring-bad/40", dot: "bg-bad" },
  ] as const;
  return (
    <div role="group" aria-label={t("Modo de reproducción")} className="flex items-center gap-0.5 rounded-lg border border-line bg-surface p-0.5">
      {options.map(({ mode, label, Icon, hint, active, dot }) => {
        const isActive = (mode === "rec") === rec;
        return (
          <button
            key={mode}
            type="button"
            aria-pressed={isActive}
            title={t(hint)}
            onClick={() => !isActive && onChange(mode)}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-semibold uppercase tracking-wide transition-colors",
              "focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent",
              isActive ? cn("ring-1 ring-inset", active) : "text-muted hover:bg-raised hover:text-ink",
            )}
          >
            <Icon className={cn("size-3.5", mode === "rec" && "fill-current")} aria-hidden />
            {t(label)}
            {isActive && <span className={cn("size-1.5 rounded-full motion-safe:animate-pulse", dot)} aria-hidden />}
          </button>
        );
      })}
    </div>
  );
}
