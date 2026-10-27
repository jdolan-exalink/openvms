import { cn } from "@/lib/cn";

export type IntegrationStatus = "connected" | "not_configured" | "error" | "unknown";

/** One backend integration of the normalized configuration; entries without an id are plain feature flags. */
export type IntegrationEntry = { id?: string; label?: string; status?: IntegrationStatus; detail?: string };

const STATUS_TEXT: Record<IntegrationStatus, string> = {
  connected: "Conectado",
  not_configured: "No configurado",
  error: "Error",
  unknown: "Sin verificar",
};

const STATUS_DOT: Record<IntegrationStatus, string> = {
  connected: "bg-ok",
  not_configured: "bg-muted",
  error: "bg-bad",
  unknown: "bg-warn",
};

type Backend = { id: string; label: string; status: IntegrationStatus; detail?: string };

/** The backend integrations (Frigate, ONVIF, Provision ISR) in display order; other entries are not shown. */
export function backendIntegrations(list: readonly IntegrationEntry[] | undefined): Backend[] {
  const out: Backend[] = [];
  for (const i of list ?? []) if (i.id && i.label && i.status) out.push({ id: i.id, label: i.label, status: i.status, detail: i.detail });
  return out;
}

type Props = { integrations: readonly IntegrationEntry[] | undefined };

/**
 * Status of each backend wired to the camera. It only reports: configuring an integration
 * happens in its own place (Frigate and ONVIF in this editor, Provision on the camera).
 */
export function IntegrationsTab({ integrations }: Props) {
  if (!integrations) return <p className="text-sm text-muted">Cargando integraciones…</p>;
  const rows = backendIntegrations(integrations);
  if (rows.length === 0) return <p className="text-sm text-muted">Sin información de integraciones.</p>;
  return (
    <ul className="flex flex-col gap-2" aria-label="Integraciones">
      {rows.map((i) => (
        <li key={i.id} data-testid={`integration-${i.id}`} className="flex flex-wrap items-center justify-between gap-2 rounded-m3-xl border border-outline-variant/60 bg-surface-2 px-4 py-3">
          <span className="text-sm font-bold text-on-surface">{i.label}</span>
          <span className="flex items-center gap-2 text-sm text-on-surface">
            <span aria-hidden className={cn("h-2 w-2 rounded-full", STATUS_DOT[i.status])} />
            {STATUS_TEXT[i.status]}
          </span>
          {i.detail && <p className="basis-full text-xs text-muted">{i.detail}</p>}
        </li>
      ))}
    </ul>
  );
}
