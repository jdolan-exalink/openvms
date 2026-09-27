import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { type CameraFilter, camerasQuery, serversQuery, sitesQuery } from "@/api/queries";
import { Empty, ErrorNote, PageHeader, Select, StatusBadge, Table, TextInput, Th } from "@/components/ui";

export function Cameras() {
  const [filter, setFilter] = useState<CameraFilter>({});
  const cameras = useQuery(camerasQuery(filter));
  const sites = useQuery(sitesQuery);
  const servers = useQuery(serversQuery);
  const siteName = new Map(sites.data?.map((s) => [s.id, s.name]));
  const serverName = new Map(servers.data?.map((s) => [s.id, s.name]));
  const update = (k: keyof CameraFilter, v: string) => setFilter((f) => ({ ...f, [k]: v || undefined }));

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader title="Cámaras" description="Solo aparecen las cámaras que tu usuario tiene permiso para ver." />
      <div className="grid gap-3 sm:grid-cols-3">
        <TextInput aria-label="Buscar cámara" placeholder="Buscar por nombre" value={filter.q ?? ""} onChange={(e) => update("q", e.target.value)} />
        <Select aria-label="Filtrar por sitio" value={filter.site_id ?? ""} onChange={(e) => update("site_id", e.target.value)}>
          <option value="">Todos los sitios</option>
          {sites.data?.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </Select>
        <Select aria-label="Filtrar por servidor" value={filter.server_id ?? ""} onChange={(e) => update("server_id", e.target.value)}>
          <option value="">Todos los servidores</option>
          {servers.data?.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </Select>
      </div>
      <ErrorNote error={cameras.error} />
      {cameras.data?.length === 0 && <Empty>No hay cámaras que coincidan.</Empty>}
      {!!cameras.data?.length && (
        <Table label="Cámaras">
          <thead>
            <tr>
              <Th>Cámara</Th>
              <Th>Sitio</Th>
              <Th>Servidor</Th>
              <Th>Estado</Th>
              <Th className="text-right">FPS</Th>
              <Th>Zonas</Th>
            </tr>
          </thead>
          <tbody>
            {cameras.data.map((c) => (
              <tr key={c.id} className="border-t border-line align-top">
                <td>
                  <div className="flex items-center gap-2 font-medium">
                    {c.display_name}
                    {c.lpr && <span className="rounded bg-raised px-1.5 py-0.5 font-mono text-[10px] text-muted">LPR</span>}
                  </div>
                  {c.display_name !== c.remote_name && <div className="font-mono text-xs text-muted">{c.remote_name}</div>}
                  {c.missing_since && <div className="text-xs text-warn">Ya no aparece en Frigate</div>}
                </td>
                <td>{siteName.get(c.site_id) ?? "—"}</td>
                <td>{serverName.get(c.server_id) ?? "—"}</td>
                <td>{c.enabled ? <StatusBadge status={c.status} /> : <span className="text-xs text-muted">Deshabilitada</span>}</td>
                <td className="text-right font-mono text-xs tabular-nums">{c.fps != null ? c.fps.toFixed(1) : "—"}</td>
                <td className="text-xs text-muted">{c.zones.join(", ") || "—"}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}
