import { ArrowLeft } from "lucide-react";
import type { ReactNode } from "react";
import { Icon } from "@/components/Icon";
import { LinkButton } from "@/components/ui";
import { cn } from "@/lib/cn";
import type { StatusChip } from "./summary";

const TONE = { ok: "text-ok", off: "text-muted", unknown: "text-muted" } as const;

/** Camera name, server/engine line, quick state chips and the screen actions. */
export function CameraHeader({ name, serverName, engine = "Frigate", chips, actions }: { name: string; serverName?: string; engine?: string; chips: StatusChip[]; actions?: ReactNode }) {
  return (
    <header className="flex flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <LinkButton to="/cameras" variant="text" className="-ml-3 self-start text-xs">
            <Icon icon={ArrowLeft} size="xs" /> Volver a cámaras
          </LinkButton>
          <h1 className="truncate text-xl font-bold tracking-tight text-ink sm:text-2xl">{name}</h1>
          <p className="text-sm text-muted">Servidor: {serverName || "—"} · Motor: {engine}</p>
        </div>
        {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
      </div>
      <ul aria-label="Estado de la cámara" className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
        {chips.map((c) => (
          <li key={c.id} className={cn("inline-flex items-center gap-1.5", TONE[c.tone])}>
            <span aria-hidden>{c.tone === "ok" ? "●" : "○"}</span>
            {c.label}
          </li>
        ))}
      </ul>
    </header>
  );
}
