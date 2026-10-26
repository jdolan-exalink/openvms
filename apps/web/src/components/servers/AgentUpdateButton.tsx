import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ApiError, api, type Schemas, unwrap } from "@/api/client";
import { useT } from "@/i18n";
import { Button } from "@/components/ui";

/**
 * AgentUpdateButton updates an installed agent through the agent itself (no SSH): the backend waits for
 * the agent to restart, so the request can take close to a minute. Error text is always localized copy,
 * never the raw API message.
 */
export function AgentUpdateButton({ server }: { server: Schemas["Server"] }) {
  const t = useT();
  const qc = useQueryClient();
  const update = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/servers/{serverId}/agent/update", { params: { path: { serverId: server.id } } })),
    onSuccess: () => Promise.all([
      qc.invalidateQueries({ queryKey: ["server-agent", server.id] }),
      qc.invalidateQueries({ queryKey: ["servers"] }),
    ]),
  });
  const unconfirmed = update.isSuccess && (update.data.binary_status === "update_available" || !!update.data.error);
  return (
    <>
      <Button size="sm" variant="outlined" disabled={update.isPending} onClick={() => update.mutate()}>
        {update.isPending ? t("servers.agentUpdating") : t("servers.agentUpdate")}
      </Button>
      {update.isPending && <span role="status" className="text-xs text-muted">{t("servers.agentUpdateHTTPDuration")}</span>}
      {unconfirmed && <span role="alert" className="text-xs text-warn">{t("servers.agentUpdateHTTPUnconfirmed")}</span>}
      {update.isError && <span role="alert" className="text-xs text-bad">{updateErrorText(update.error, t)}</span>}
    </>
  );
}

function updateErrorText(error: unknown, t: ReturnType<typeof useT>) {
  if (error instanceof ApiError) {
    if (error.status === 502) return t("servers.agentUpdateHTTPUnreachable");
    if (error.code === "forbidden") return t("servers.agentUpdateHTTPForbidden");
  }
  return t("servers.agentUpdateHTTPFailed");
}
