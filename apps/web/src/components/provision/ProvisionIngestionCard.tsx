import { useQuery } from "@tanstack/react-query";
import { requestJson } from "@/api/client";
import { ErrorNote } from "@/components/ui";
import { fmtDateTime } from "@/lib/format";

/** Ingestion is how far OpenVMS got collecting the plate readings a server's agent queued. */
export interface Ingestion {
  known: boolean;
  last_seq: number;
  ingested_total: number;
  skipped_total: number;
  agent_lost: number;
  agent_depth: number;
  last_pull_at?: string;
  last_error?: string;
}

const ingestionPath = (serverId: string) => `/api/v1/servers/${encodeURIComponent(serverId)}/provision-lpr/ingestion`;

/**
 * ProvisionIngestionCard answers "are the readings of the cameras reaching OpenVMS?": when the agent
 * was last asked, how many readings it still holds, how many were stored and how many were dropped on
 * either side. Dropped readings and the last error are shown as warnings because they mean data that
 * will not be in the plate search.
 */
export function ProvisionIngestionCard({ serverId }: { serverId: string }) {
  const state = useQuery({
    queryKey: ["provision-ingestion", serverId],
    queryFn: () => requestJson<Ingestion>("GET", ingestionPath(serverId)),
    refetchInterval: 15_000,
    retry: false,
  });
  const data = state.data;

  return (
    <div className="flex flex-col gap-3 rounded-m3-2xl border border-outline-variant/50 bg-surface-1 p-5 shadow-sm">
      <h3 className="text-sm font-bold text-on-surface">Lecturas de patentes de las cámaras</h3>

      {state.error && <ErrorNote error={state.error instanceof Error ? state.error : new Error(String(state.error))} />}

      {data && !data.known && (
        <p className="text-xs text-on-surface-variant">Todavía no se consultó el agente de este servidor. OpenVMS lo consulta cada pocos segundos.</p>
      )}

      {data?.known && (
        <>
          <ul className="flex flex-col gap-1 text-xs text-on-surface-variant">
            <li>Última consulta al agente: {data.last_pull_at ? fmtDateTime(data.last_pull_at) : "—"}</li>
            <li>Pendientes en el agente: {data.agent_depth}</li>
            <li>Guardadas en OpenVMS: {data.ingested_total}</li>
            <li>
              Descartadas: {data.skipped_total}
              {data.skipped_total > 0 && " (cámara no registrada en OpenVMS o sin patente legible)"}
            </li>
          </ul>

          {data.agent_lost > 0 && (
            <p role="status" className="rounded-m3-lg bg-warn/15 p-2 text-xs text-warn">
              {data.agent_lost} lecturas se perdieron en el agente porque su cola se llenó antes de que OpenVMS las leyera.
            </p>
          )}

          {data.last_error && (
            <p role="alert" className="rounded-m3-lg bg-warn/15 p-2 text-xs text-warn">
              {data.last_error}
            </p>
          )}
        </>
      )}
    </div>
  );
}
