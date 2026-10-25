import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Schemas, unwrap } from "@/api/client";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { ConnectAgentModal } from "@/components/servers/ConnectAgentModal";
import { Button } from "@/components/ui";


/**
 * ConnectAgentControls is the server row's way to register an agent that already runs elsewhere, and to
 * forget one that was registered that way. An agent installed over SSH is not shown here as connected:
 * it has its own install, update and uninstall actions.
 */
export function ConnectAgentControls({ server }: { server: Schemas["Server"] }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const state = useQuery({
    queryKey: ["server-agent-connection", server.id],
    queryFn: async () => unwrap(await api.GET("/api/v1/servers/{serverId}/agent/connection", { params: { path: { serverId: server.id } } })),
    retry: false,
  });
  const disconnect = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/api/v1/servers/{serverId}/agent/connection", { params: { path: { serverId: server.id } } })),
    onSuccess: async () => {
      setConfirming(false);
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["server-agent-connection", server.id] }),
        qc.invalidateQueries({ queryKey: ["server-agent", server.id] }),
      ]);
    },
  });
  const connection = state.data;
  const close = () => {
    setOpen(false);
    void qc.invalidateQueries({ queryKey: ["server-agent-connection", server.id] });
  };

  return (
    <>
      {connection?.connected ? (
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted">
          <span>
            Agente conectado por dirección: <span className="font-mono text-on-surface">{connection.base_url}</span>
            {connection.has_headers && " · con cabeceras de gateway"}
            {connection.has_custom_ca && " · con autoridad propia"}
          </span>
          <Button size="sm" variant="outlined" onClick={() => setOpen(true)}>
            Cambiar
          </Button>
          <Button size="sm" variant="outlined" className="text-bad" onClick={() => setConfirming(true)}>
            Desconectar
          </Button>
        </div>
      ) : (
        <Button size="sm" variant="outlined" onClick={() => setOpen(true)}>
          Conectar agente existente
        </Button>
      )}
      {open && <ConnectAgentModal server={server} onClose={close} />}
      {confirming && (
        <ConfirmDialog
          title="Desconectar agente"
          message="OpenVMS deja de consultar este agente. El agente sigue instalado en su equipo y se puede volver a conectar."
          confirmLabel="Desconectar agente"
          pending={disconnect.isPending}
          error={disconnect.error}
          onConfirm={() => disconnect.mutate()}
          onCancel={() => setConfirming(false)}
        />
      )}
    </>
  );
}
