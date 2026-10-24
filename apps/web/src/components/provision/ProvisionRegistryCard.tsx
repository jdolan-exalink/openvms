import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, requestJson } from "@/api/client";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Button, ErrorNote } from "@/components/ui";
import { fmtDateTime } from "@/lib/format";

interface UnregisteredSource {
  ip: string;
  last_seen: string;
  requests: number;
  last_path: string;
  rejected: boolean;
}

/** Registry is the agent's list of cameras allowed to report, as the API returns it. */
export interface Registry {
  enforce: boolean;
  cameras: { camera_id: string; ip: string }[];
  unregistered_seen: UnregisteredSource[];
  load_error?: string;
}

const registryPath = (serverId: string) => `/api/v1/servers/${encodeURIComponent(serverId)}/provision-lpr/registry`;
const enforcePath = (serverId: string) => `/api/v1/servers/${encodeURIComponent(serverId)}/provision-lpr/enforce`;

/**
 * ProvisionRegistryCard shows which cameras the server's agent accepts. Until enforcement is turned on
 * the agent accepts any camera on the network, so the card lists the addresses that sent data without
 * being registered, which is what has to be registered first. Turning enforcement on asks first,
 * because it stops the data of every address that is not registered.
 */
export function ProvisionRegistryCard({ serverId }: { serverId: string }) {
  const qc = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const registry = useQuery({
    queryKey: ["provision-registry", serverId],
    queryFn: () => requestJson<Registry>("GET", registryPath(serverId)),
    refetchInterval: 10_000,
    retry: false,
  });
  const setEnforce = useMutation({
    mutationFn: (enforce: boolean) => requestJson<Registry>("PUT", enforcePath(serverId), { enforce }),
    onSuccess: async (data) => {
      setConfirming(false);
      qc.setQueryData(["provision-registry", serverId], data);
      await qc.invalidateQueries({ queryKey: ["provision-registry", serverId] });
    },
  });

  const data = registry.data;
  const outdated = registry.error instanceof ApiError && registry.error.code === "agent_outdated";
  const unregistered = data?.unregistered_seen ?? [];

  return (
    <div className="flex flex-col gap-3 rounded-m3-2xl border border-outline-variant/50 bg-surface-1 p-5 shadow-sm">
      <h3 className="text-sm font-bold text-on-surface">Cámaras permitidas en el receptor</h3>

      {outdated && <p className="text-xs text-muted">El agente de este servidor es anterior al registro de cámaras. Actualizalo para poder exigir cámaras registradas.</p>}
      {registry.error && !outdated && <ErrorNote error={registry.error instanceof Error ? registry.error : new Error(String(registry.error))} />}

      {data?.load_error && (
        <p role="alert" className="rounded-m3-lg bg-warn/15 p-2 text-xs text-warn">
          El registro guardado en el agente no se pudo leer: el agente rechaza todas las cámaras hasta que se repare en el equipo.
        </p>
      )}

      {data && (
        <>
          <p className="text-xs text-on-surface-variant">
            {data.enforce
              ? "Exigido: solo las cámaras registradas pueden enviar datos al agente."
              : "Sin exigir: el receptor acepta datos de cualquier cámara de la red. Registrá las cámaras y después activá la exigencia."}{" "}
            Registradas: {data.cameras.length}.
          </p>

          {unregistered.length > 0 && (
            <div className="flex flex-col gap-1.5">
              <span className="text-[11px] font-bold uppercase text-muted">Direcciones que enviaron datos sin estar registradas</span>
              <ul className="flex flex-col gap-1">
                {unregistered.map((s) => (
                  <li key={s.ip} className="flex flex-wrap items-center justify-between gap-2 rounded-m3-lg bg-surface-2 px-3 py-2 text-xs">
                    <span className="font-mono font-bold text-on-surface">{s.ip}</span>
                    <span className="text-muted">
                      {s.requests} {s.requests === 1 ? "pedido" : "pedidos"} · {s.last_path} · {fmtDateTime(s.last_seen)}
                    </span>
                    {s.rejected && <span className="rounded-m3-sm bg-warn/15 px-1.5 py-0.5 text-[10px] font-semibold text-warn">rechazada</span>}
                  </li>
                ))}
              </ul>
            </div>
          )}

          <Button
            variant={data.enforce ? "text" : "tonal"}
            disabled={setEnforce.isPending || Boolean(data.load_error)}
            onClick={() => (data.enforce ? setEnforce.mutate(false) : setConfirming(true))}
          >
            {data.enforce ? "Dejar de exigir cámaras registradas" : "Exigir cámaras registradas"}
          </Button>
          {!confirming && <ErrorNote error={setEnforce.error} />}
        </>
      )}

      {confirming && (
        <ConfirmDialog
          title="Exigir cámaras registradas"
          message={
            unregistered.length > 0
              ? `Dejarán de aceptarse los datos de ${unregistered.length} ${unregistered.length === 1 ? "dirección" : "direcciones"} que hoy no están registradas: ${unregistered.map((s) => s.ip).join(", ")}. Registrá primero las cámaras que quieras conservar.`
              : "Solo las cámaras registradas podrán enviar datos al agente. Una cámara nueva tendrá que registrarse antes de que se acepten sus datos."
          }
          confirmLabel="Exigir"
          pending={setEnforce.isPending}
          error={setEnforce.error}
          onConfirm={() => setEnforce.mutate(true)}
          onCancel={() => setConfirming(false)}
        />
      )}
    </div>
  );
}
