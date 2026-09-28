import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { History } from "lucide-react";
import { useState } from "react";
import type { Schemas } from "@/api/client";
import { cameraGroupsQuery, meQuery, type PlateFilter, platesQuery, sitesQuery } from "@/api/queries";
import { PlateDetailModal } from "@/components/PlateDetailModal";
import { Button, Empty, ErrorNote, Field, PageHeader, Select, Table, TextInput, Th } from "@/components/ui";
import { fmtDateTime, fromLocalInput, labelName } from "@/lib/format";
import { can } from "@/lib/perm";

/**
 * PlatePreview shows the Frigate tracked-object snapshot for one plate read (its own
 * remote_event_id, not the event's detections[0] — see internal/media/gateway.go
 * lprReadSnapshot), lazily requested only while the row is hovered or focused.
 */
function PlatePreview({ read }: { read: Schemas["PlateRead"] }) {
  const [status, setStatus] = useState<"loading" | "loaded" | "error">("loading");
  return (
    <div role="tooltip" className="absolute left-0 top-full z-20 mt-1 w-80 rounded border border-line bg-surface p-2 shadow-lg">
      {status === "loading" && <p className="text-xs text-muted">Cargando foto…</p>}
      {status === "error" ? (
        <p className="text-xs text-muted">No se pudo cargar la foto.</p>
      ) : (
        <img
          src={`/media/v1/lpr/reads/${read.id}/snapshot.jpg`}
          alt={`Foto de la lectura de patente ${read.plate_normalized}`}
          className="w-full rounded"
          onLoad={() => setStatus("loaded")}
          onError={() => setStatus("error")}
        />
      )}
    </div>
  );
}

/** Plates is the global LPR search (PRD §41-45) across every authorized Frigate. */
export function Plates() {
  const me = useQuery(meQuery);
  const sites = useQuery(sitesQuery);
  const cameraGroups = useQuery(cameraGroupsQuery);
  const [form, setForm] = useState({ plate: "", exact: false, site: "", cameraGroup: "", from: "", to: "" });
  const [filter, setFilter] = useState<PlateFilter>({});
  const reads = useInfiniteQuery(platesQuery(filter));
  const items = reads.data?.pages.flatMap((p) => p.items) ?? [];
  const [hoveredId, setHoveredId] = useState<string | null>(null);
  const [detailRead, setDetailRead] = useState<Schemas["PlateRead"] | null>(null);
  const canPreviewSnapshot = can(me.data, "snapshots.view") && can(me.data, "lpr.view");
  const canViewDetail = can(me.data, "lpr.view") && (can(me.data, "snapshots.view") || can(me.data, "recordings.view"));

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader title="Patentes" description="Lecturas de patentes de todos los servidores. Los espacios y guiones se ignoran al buscar." />
      <form
        className="grid gap-3 rounded border border-line bg-surface p-3 sm:grid-cols-2 lg:grid-cols-5"
        onSubmit={(e) => {
          e.preventDefault();
          setFilter({
            plate: form.plate.trim() || undefined,
            exact: form.exact || undefined,
            site_id: form.site ? [form.site] : undefined,
            camera_group_id: form.cameraGroup ? [form.cameraGroup] : undefined,
            from: fromLocalInput(form.from),
            to: fromLocalInput(form.to),
          });
        }}
      >
        <Field label="Patente">
          <TextInput
            autoFocus
            value={form.plate}
            disabled={!can(me.data, "lpr.search")}
            onChange={(e) => setForm({ ...form, plate: e.target.value.toUpperCase() })}
            placeholder="AB123CD o parte"
          />
        </Field>
        <Field label="Sitio">
          <Select value={form.site} onChange={(e) => setForm({ ...form, site: e.target.value })}>
            <option value="">Todos</option>
            {sites.data?.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Grupo de cámaras">
          <Select value={form.cameraGroup} onChange={(e) => setForm({ ...form, cameraGroup: e.target.value })}>
            <option value="">Todos</option>
            {cameraGroups.data?.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Desde">
          <TextInput type="datetime-local" value={form.from} onChange={(e) => setForm({ ...form, from: e.target.value })} />
        </Field>
        <Field label="Hasta">
          <TextInput type="datetime-local" value={form.to} onChange={(e) => setForm({ ...form, to: e.target.value })} />
        </Field>
        <div className="flex flex-col justify-end gap-2">
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={form.exact} onChange={(e) => setForm({ ...form, exact: e.target.checked })} /> Coincidencia exacta
          </label>
          <Button type="submit" variant="primary">
            Buscar
          </Button>
        </div>
      </form>
      <ErrorNote error={reads.error} />
      {reads.isSuccess && items.length === 0 && <Empty>No hay lecturas que coincidan.</Empty>}
      {items.length > 0 && (
        <Table label="Lecturas de patentes">
          <thead>
            <tr>
              <Th>Patente</Th>
              <Th>Fecha</Th>
              <Th>Cámara</Th>
              <Th>Sitio</Th>
              <Th className="text-right">Confianza</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {items.map((r) => (
              <tr key={r.id} className="border-t border-line">
                <td className="relative">
                  <span
                    className="rounded bg-raised px-2 py-0.5 font-mono font-medium tracking-wider"
                    {...(canPreviewSnapshot
                      ? {
                          tabIndex: 0,
                          onMouseEnter: () => setHoveredId(r.id),
                          onMouseLeave: () => setHoveredId((id) => (id === r.id ? null : id)),
                          onFocus: () => setHoveredId(r.id),
                          onBlur: () => setHoveredId((id) => (id === r.id ? null : id)),
                        }
                      : {})}
                  >
                    {r.plate_normalized}
                  </span>
                  <span className="ml-2 text-xs text-muted">{labelName(r.label)}</span>
                  {canPreviewSnapshot && hoveredId === r.id && <PlatePreview read={r} />}
                </td>
                <td className="whitespace-nowrap">{fmtDateTime(r.seen_at)}</td>
                <td>{r.camera_name}</td>
                <td>
                  {r.site_name}
                  <div className="text-xs text-muted">{r.server_name}</div>
                </td>
                <td className="text-right tabular-nums">{r.score != null ? `${Math.round(r.score * 100)}%` : "—"}</td>
                <td className="text-right whitespace-nowrap">
                  {canViewDetail && (
                    <Button className="mr-2" onClick={() => setDetailRead(r)}>
                      Ver detalle
                    </Button>
                  )}
                  {can(me.data, "recordings.view") && (
                    <Link
                      to="/playback"
                      search={{ camera: r.camera_id, t: Math.floor(new Date(r.seen_at).getTime() / 1000) - 5 }}
                      className="inline-flex items-center gap-1 text-xs text-accent hover:underline"
                    >
                      <History className="size-3.5" aria-hidden /> Grabación
                    </Link>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {reads.hasNextPage && (
        <Button onClick={() => void reads.fetchNextPage()} disabled={reads.isFetchingNextPage} className="self-center">
          {reads.isFetchingNextPage ? "Cargando…" : "Cargar más"}
        </Button>
      )}
      {detailRead && <PlateDetailModal read={detailRead} onClose={() => setDetailRead(null)} />}
    </div>
  );
}
