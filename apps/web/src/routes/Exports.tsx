import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { exportsQuery } from "@/api/queries";
import { Icon } from "@/components/Icon";
import { Empty, ErrorNote, IconButton, PageHeader, Table, Th } from "@/components/ui";
import { fmtDateTime } from "@/lib/format";

const statusText: Record<string, string> = { pending: "En cola", running: "Generando", ready: "Lista", failed: "Falló" };

/** Exports lists clips requested through the VMS; files stay in the origin Frigate. */
export function Exports() {
  const t = useT();
  const qc = useQueryClient();
  const exports = useQuery(exportsQuery);
  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/api/v1/exports/{exportId}", { params: { path: { exportId: id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["exports"] }),
  });

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader title={t("nav.exports")} description={t("settings.exports")} />
      <ErrorNote error={exports.error ?? remove.error} />
      {exports.data?.length === 0 && <Empty>Todavía no hay exportaciones.</Empty>}
      {!!exports.data?.length && (
        <Table label="Exportaciones">
          <thead>
            <tr>
              <Th>Nombre</Th>
              <Th>Cámara</Th>
              <Th>Rango</Th>
              <Th>Pedida por</Th>
              <Th>Estado</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {exports.data.map((x) => (
              <tr key={x.id} className="border-t border-outline-variant align-top">
                <td className="font-medium">{x.name}</td>
                <td>{x.camera_name}</td>
                <td className="font-mono text-xs whitespace-nowrap">
                  {fmtDateTime(x.start_time)}
                  <br />
                  {fmtDateTime(x.end_time)}
                </td>
                <td className="text-xs">
                  {x.requested_by_name}
                  <div className="text-muted">{fmtDateTime(x.created_at)}</div>
                </td>
                <td className="text-sm">
                  <span className={`inline-flex h-6 items-center rounded-full bg-surface-2 px-2.5 text-xs font-medium ${x.status === "ready" ? "text-ok" : x.status === "failed" ? "text-bad" : "text-warn"}`}>{statusText[x.status]}</span>
                  {x.status === "running" && x.progress > 0 && <span className="ml-1 text-xs text-muted">{Math.round(x.progress)}%</span>}
                  {x.error && <div role="alert" className="max-w-56 text-xs text-bad">{x.error}</div>}
                </td>
                <td className="text-right whitespace-nowrap">
                  <div className="inline-flex items-center gap-2">
                  {x.status === "ready" && (
                    <a
                      href={`/media/v1/exports/${x.id}/download`}
                      className="m3-press inline-flex h-9 items-center justify-center gap-2 rounded-full bg-primary px-4 text-sm font-bold text-on-primary hover:brightness-110 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
                    >
                      <Icon icon={Download} size="xs" /> Descargar
                    </a>
                  )}
                  <IconButton icon={Trash2} onClick={() => remove.mutate(x.id)} aria-label="Quitar de la lista" title="Quitar de la lista" />
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}
