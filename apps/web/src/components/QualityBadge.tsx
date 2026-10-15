import { cn } from "@/lib/cn";
import type { Quality } from "@/lib/liveGrid";

const LABEL: Record<Quality, string> = { sub: "SUB", main: "MAIN" };
const NAME: Record<Quality, string> = { sub: "secundaria", main: "principal" };

/**
 * QualityBadge tells which stream a live tile is actually showing. `visible` is the quality on
 * screen; `pending` is a target quality that was requested but has no drawable frame yet (the
 * handoff in progress). It is small, low contrast and always visible, so it does not follow
 * the 3 s overlay auto-hide and never covers the video controls.
 */
export function QualityBadge({ visible, pending, className }: { visible: Quality; pending: Quality | null; className?: string }) {
  const switching = pending !== null && pending !== visible;
  const label = switching ? `Calidad: ${NAME[visible]}, cambiando a ${NAME[pending]}` : `Calidad: ${NAME[visible]}`;
  return (
    <span
      data-testid="quality-badge"
      data-quality={switching ? "switching" : visible}
      role="img"
      aria-label={label}
      title={label}
      className={cn(
        "inline-flex shrink-0 items-center rounded px-1.5 py-0.5 font-mono text-[10px] font-semibold leading-none tracking-wide backdrop-blur",
        switching
          ? "bg-warn/25 text-warn motion-safe:animate-pulse"
          : visible === "main"
            ? "bg-primary/80 text-on-primary"
            : "bg-surface-1/70 text-on-surface-variant",
        className,
      )}
    >
      {switching ? `${LABEL[visible]} → ${LABEL[pending]}` : LABEL[visible]}
    </span>
  );
}
