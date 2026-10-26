import { Settings, Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import type { Schemas } from "@/api/client";
import { Icon } from "@/components/Icon";
import { ConnectAgentControls } from "@/components/servers/ConnectAgentControls";
import { Button } from "@/components/ui";
import { useT } from "@/i18n";

/**
 * ServerActionsPanel groups every server action that is not on the server row: SSH install, connecting
 * an existing agent, restart, sync, the Frigate config editor and delete. It lives inside the edit
 * modal, outside its <form>, so none of these buttons can submit the form. Permission gates are
 * decided by the caller; the install, restart and sync controls arrive as slots because they own
 * state that lives with the Servers screen.
 */
export function ServerActionsPanel({
  server,
  canInstall,
  installPermissionMissing,
  canManage,
  canRestart,
  installControls,
  restartControl,
  syncControl,
  onConfigureFrigate,
  onDelete,
}: {
  server: Schemas["Server"];
  canInstall: boolean;
  installPermissionMissing: boolean;
  canManage: boolean;
  canRestart: boolean;
  installControls: ReactNode;
  restartControl: ReactNode;
  syncControl: ReactNode;
  onConfigureFrigate: (server: Schemas["Server"]) => void;
  onDelete: (server: Schemas["Server"]) => void;
}) {
  const t = useT();
  return (
    <section aria-label={t("servers.actionsTitle")} className="flex flex-col gap-3 border-t border-outline/30 pt-4">
      <h3 className="text-sm font-bold">{t("servers.actionsTitle")}</h3>
      <div className="flex flex-wrap items-start gap-2">
        {canInstall && installControls}
        {canInstall && <ConnectAgentControls key={`connect-${server.id}`} server={server} />}
        {installPermissionMissing && (
          <span role="note" className="text-xs text-muted">{t("servers.agentInstallPermissionRequired")}</span>
        )}
        {canRestart && restartControl}
        {canManage && syncControl}
        {canManage && (
          <Button
            size="sm"
            variant="tonal"
            onClick={() => onConfigureFrigate(server)}
            title="Configuración global de Frigate y parámetros del sistema"
          >
            <Icon icon={Settings} size="xs" />
            Configurar Frigate
          </Button>
        )}
        {canManage && (
          <Button size="sm" variant="outlined" className="text-bad" onClick={() => onDelete(server)} title="Eliminar servidor">
            <Icon icon={Trash2} size="xs" />
            Eliminar
          </Button>
        )}
      </div>
    </section>
  );
}
