import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { frigateRevisionsQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Modal } from "@/components/Modal";
import { Button, Empty, ErrorNote } from "@/components/ui";
import { fmtDateTime } from "@/lib/format";
import { sectionLabel } from "@/lib/frigateSchema";
import { YamlDiff } from "@/components/frigate/YamlDiff";

type Revision = Schemas["FrigateConfigRevision"];

export const kindLabel: Record<Revision["kind"], string> = {
  camera_patch: "Cambio de cámara",
  server_patch: "Cambio del servidor",
  raw_save: "Edición del YAML",
  rollback: "Restauración",
};

function RevisionDetail({ rev, onClose }: { rev: Revision; onClose: () => void }) {
  return (
    <Modal title="Detalle del cambio" onClose={onClose}>
      <p className="text-sm text-muted">
        {fmtDateTime(rev.created_at)} · {rev.actor_name} · {kindLabel[rev.kind]}
      </p>
      <h3 className="text-sm font-semibold">Parche enviado</h3>
      <pre className="max-h-60 overflow-auto rounded-m3-md bg-surface-2 p-3 font-mono text-xs">{JSON.stringify(rev.patch, null, 2)}</pre>
      {rev.before_yaml !== undefined && rev.after_yaml !== undefined ? (
        <>
          <h3 className="text-sm font-semibold">Archivo de configuración: antes → después</h3>
          <YamlDiff before={rev.before_yaml} after={rev.after_yaml} />
        </>
      ) : (
        <p className="text-xs text-muted">El diff del YAML requiere permiso de credenciales.</p>
      )}
    </Modal>
  );
}

/** FrigateHistory lists the stored revisions of a camera's config and restores one. */
export function FrigateHistory({
  serverId, cameraId, secretsVisible, canRollback, onRestored,
}: {
  serverId: string;
  cameraId: string;
  secretsVisible: boolean;
  canRollback: boolean;
  onRestored: () => void;
}) {
  const qc = useQueryClient();
  const revisions = useQuery(frigateRevisionsQuery(serverId, cameraId, secretsVisible));
  const [viewing, setViewing] = useState<Revision>();
  const [restoring, setRestoring] = useState<Revision>();
  const rollback = useMutation({
    mutationFn: async (rev: Revision) =>
      unwrap(await api.POST("/api/v1/servers/{serverId}/frigate-config/revisions/{revisionId}/rollback", { params: { path: { serverId, revisionId: rev.id } } })),
    onSuccess: async () => {
      setRestoring(undefined);
      onRestored();
      await qc.invalidateQueries({ queryKey: ["servers", serverId, "frigate-revisions"] });
      await qc.invalidateQueries({ queryKey: ["cameras", cameraId] });
    },
  });

  return (
    <div className="flex flex-col gap-3">
      <h2 className="text-lg font-semibold">Historial de cambios</h2>
      <ErrorNote error={revisions.error} />
      {revisions.isLoading && <p className="text-sm text-muted">Cargando historial…</p>}
      {revisions.data?.length === 0 && <Empty>Aún no hay cambios guardados para esta cámara.</Empty>}
      <ul className="flex flex-col gap-2">
        {revisions.data?.map((rev) => (
          <li key={rev.id} className="flex flex-wrap items-center justify-between gap-2 rounded-m3-lg bg-surface-2 px-4 py-3 text-sm">
            <div className="flex flex-col gap-1">
              <span>
                <span className="font-medium">{kindLabel[rev.kind]}</span> · {rev.actor_name}
              </span>
              <span className="text-xs text-muted">{fmtDateTime(rev.created_at)}</span>
              <span className="flex flex-wrap gap-1">
                {rev.sections.map((s) => (
                  <span key={s} className="rounded-full bg-secondary-container px-2 py-0.5 text-[11px] text-on-secondary-container">{sectionLabel(s)}</span>
                ))}
              </span>
            </div>
            <div className="flex gap-2">
              <Button size="sm" aria-label={`Ver cambio del ${fmtDateTime(rev.created_at)}`} onClick={() => setViewing(rev)}>Ver cambio</Button>
              {canRollback && (
                <Button size="sm" aria-label={`Restaurar versión anterior al ${fmtDateTime(rev.created_at)}`} onClick={() => setRestoring(rev)}>
                  Restaurar esta versión
                </Button>
              )}
            </div>
          </li>
        ))}
      </ul>
      {viewing && <RevisionDetail rev={viewing} onClose={() => setViewing(undefined)} />}
      {restoring && (
        <ConfirmDialog
          title="Restaurar esta versión"
          message="Se restaurará el archivo de configuración completo del servidor a como estaba antes de este cambio (afecta a todas las cámaras de este servidor, no solo a esta) y requiere reiniciar Frigate para aplicarse."
          confirmLabel="Restaurar"
          pending={rollback.isPending}
          error={rollback.error}
          onConfirm={() => rollback.mutate(restoring)}
          onCancel={() => setRestoring(undefined)}
        />
      )}
    </div>
  );
}
