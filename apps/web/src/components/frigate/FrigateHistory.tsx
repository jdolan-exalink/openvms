import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { frigateRevisionsQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Modal } from "@/components/Modal";
import { Button, Empty, ErrorNote } from "@/components/ui";
import { cn } from "@/lib/cn";
import { fmtDateTime } from "@/lib/format";
import { type DiffLine, lineDiff, sectionLabel } from "@/lib/frigateSchema";

type Revision = Schemas["FrigateConfigRevision"];

const kindLabel: Record<Revision["kind"], string> = {
  camera_patch: "Cambio de cámara",
  raw_save: "Edición del YAML",
  rollback: "Restauración",
};

/** collapse keeps changed lines with a little context and folds the long unchanged runs. */
function collapse(lines: DiffLine[], context = 2): (DiffLine | { kind: "gap"; count: number })[] {
  const keep = new Set<number>();
  lines.forEach((l, i) => {
    if (l.kind === "same") return;
    for (let k = Math.max(0, i - context); k <= Math.min(lines.length - 1, i + context); k++) keep.add(k);
  });
  const out: (DiffLine | { kind: "gap"; count: number })[] = [];
  let gap = 0;
  lines.forEach((l, i) => {
    if (keep.has(i)) {
      if (gap) out.push({ kind: "gap", count: gap });
      gap = 0;
      out.push(l);
    } else {
      gap++;
    }
  });
  if (gap) out.push({ kind: "gap", count: gap });
  return out;
}

function YamlDiff({ before, after }: { before: string; after: string }) {
  const rows = useMemo(() => collapse(lineDiff(before, after)), [before, after]);
  if (!rows.some((r) => r.kind !== "gap")) return <p className="text-sm text-muted">El archivo no cambió.</p>;
  return (
    <pre aria-label="Diferencias del YAML" className="max-h-96 overflow-auto rounded border border-line bg-bg p-2 font-mono text-xs">
      {rows.map((r, i) =>
        r.kind === "gap" ? (
          <div key={i} className="text-muted">… {r.count} líneas sin cambios …</div>
        ) : (
          <div key={i} className={cn("whitespace-pre-wrap", r.kind === "add" && "bg-ok/15 text-ok", r.kind === "del" && "bg-bad/15 text-bad")}>
            {r.kind === "add" ? "+ " : r.kind === "del" ? "- " : "  "}
            {r.text}
          </div>
        ),
      )}
    </pre>
  );
}

function RevisionDetail({ rev, onClose }: { rev: Revision; onClose: () => void }) {
  return (
    <Modal title="Detalle del cambio" onClose={onClose}>
      <p className="text-sm text-muted">
        {fmtDateTime(rev.created_at)} · {rev.actor_name} · {kindLabel[rev.kind]}
      </p>
      <h3 className="text-sm font-semibold">Parche enviado</h3>
      <pre className="max-h-60 overflow-auto rounded border border-line bg-bg p-2 font-mono text-xs">{JSON.stringify(rev.patch, null, 2)}</pre>
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
          <li key={rev.id} className="flex flex-wrap items-center justify-between gap-2 rounded border border-line px-3 py-2 text-sm">
            <div className="flex flex-col gap-1">
              <span>
                <span className="font-medium">{kindLabel[rev.kind]}</span> · {rev.actor_name}
              </span>
              <span className="text-xs text-muted">{fmtDateTime(rev.created_at)}</span>
              <span className="flex flex-wrap gap-1">
                {rev.sections.map((s) => (
                  <span key={s} className="rounded bg-raised px-1.5 py-0.5 text-[11px]">{sectionLabel(s)}</span>
                ))}
              </span>
            </div>
            <div className="flex gap-2">
              <Button aria-label={`Ver cambio del ${fmtDateTime(rev.created_at)}`} onClick={() => setViewing(rev)}>Ver cambio</Button>
              {canRollback && (
                <Button aria-label={`Restaurar versión anterior al ${fmtDateTime(rev.created_at)}`} onClick={() => setRestoring(rev)}>
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
