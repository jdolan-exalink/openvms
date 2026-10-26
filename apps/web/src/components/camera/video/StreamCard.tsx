import type { ReactNode } from "react";
import { NONE } from "./videoParams";

/** One stream of the camera, spoken in video terms: what it is for and what it is carrying now. */
export function StreamCard({ title, purpose, summary, children }: { title: string; purpose: string; summary?: string; children?: ReactNode }) {
  return (
    <section aria-label={title} className="flex min-w-0 flex-col gap-3 rounded-m3-lg border border-outline-variant/50 bg-surface-2 p-4">
      <header className="flex flex-col gap-0.5">
        <h3 className="text-sm font-semibold">{title}</h3>
        <p className="text-xs text-muted">{purpose}</p>
        <p className="font-mono text-xs text-ink" data-testid={`summary-${title}`}>{summary ?? NONE}</p>
      </header>
      {children}
    </section>
  );
}
