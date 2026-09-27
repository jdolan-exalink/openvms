import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { exportsQuery } from "@/api/queries";
import { Button, Empty, ErrorNote, PageHeader, Table, Th } from "@/components/ui";
import { fmtDateTime } from "@/lib/format";

const statusText: Record<string, string> = { pending: "En cola", running: "Generando", ready: "Lista", failed: "Falló" };

/** Exports lists clips requested through the VMS; files stay in the origin Frigate. */
export function Exports() {
  const qc = useQueryClient();
  const exports = useQuery(exportsQuery);
  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/api/v1/exports/{exportId}", { params: { path: { exportId: id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["exports"] }),
  });

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader title="Exportaciones" description="Clips pedidos desde Eventos o Grabaciones. Frigate los genera y el VMS los descarga por vos." />
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
              <tr key={x.id} className="border-t border-line align-top">
                <td className="font-medium">{x.name}</td>
                <td>{x.camera_name}</td>
                <td className="text-xs whitespace-nowrap">
                  {fmtDateTime(x.start_time)}
                  <br />
                  {fmtDateTime(x.end_time)}
                </td>
                <td className="text-xs">
                  {x.requested_by_name}
                  <div className="text-muted">{fmtDateTime(x.created_at)}</div>
                </td>
                <td className="text-sm">
                  <span className={x.status === "ready" ? "text-ok" : x.status === "failed" ? "text-bad" : "text-warn"}>{statusText[x.status]}</span>
                  {x.status === "running" && x.progress > 0 && <span className="ml-1 text-xs text-muted">{Math.round(x.progress)}%</span>}
                  {x.error && <div className="max-w-56 text-xs text-bad">{x.error}</div>}
                </td>
                <td className="text-right whitespace-nowrap">
                  {x.status === "ready" && (
                    <a
                      href={`/media/v1/exports/${x.id}/download`}
                      className="mr-2 inline-flex items-center gap-1 rounded bg-accent px-2 py-1 text-xs font-medium text-bg hover:bg-accent/90"
                    >
                      <Download className="size-3.5" aria-hidden /> Descargar
                    </a>
                  )}
                  <Button onClick={() => remove.mutate(x.id)} aria-label="Quitar de la lista" title="Quitar de la lista">
                    <Trash2 className="size-3.5" aria-hidden />
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}
