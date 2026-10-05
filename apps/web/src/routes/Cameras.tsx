import { useT } from "@/i18n";
import { useQuery } from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import { Settings } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { type CameraFilter, camerasQuery, classifyPolicyQuery, meQuery, serversQuery, sitesQuery } from "@/api/queries";
import { api, unwrap } from "@/api/client";
import { BodyClassifySwitch } from "@/components/BodyClassifySwitch";
import { can } from "@/lib/perm";
import { Icon } from "@/components/Icon";
import { Empty, ErrorNote, LinkButton, PageHeader, Select, StatusBadge, Summary, Table, TextInput, Th } from "@/components/ui";
function summarize(cameras: { enabled: boolean; status: string }[]) {
  const online = cameras.filter((c) => c.enabled && c.status === "online").length;
  const disabled = cameras.filter((c) => !c.enabled).length;
  const parts = [`${cameras.length} ${cameras.length === 1 ? "cámara" : "cámaras"}`, `${online} en línea`];
  if (disabled) parts.push(`${disabled} ${disabled === 1 ? "deshabilitada" : "deshabilitadas"}`);
  return parts.join(" · ");
}

function canManageServer(me: { tenant_id: string | null; grants: { permission: string; effect: string; scope_type: string; scope_id?: string }[] } | undefined, server: { id: string; site_id: string }) {
  if (!me) return false;
  const applies = (grant: (typeof me.grants)[number]) => {
    if (grant.permission !== "servers.manage") return false;
    if (grant.scope_type === "platform") return true;
    if (grant.scope_type === "tenant") return grant.scope_id === me.tenant_id;
    if (grant.scope_type === "site") return grant.scope_id === server.site_id;
    if (grant.scope_type === "server") return grant.scope_id === server.id;
    return false;
  };
  const grants = me.grants.filter(applies);
  return grants.some((grant) => grant.effect === "allow") && !grants.some((grant) => grant.effect === "deny");
}

function displayEndpoint(endpoint: string) {
  try {
    const url = new URL(endpoint);
    if ((url.protocol !== "http:" && url.protocol !== "https:") || url.username || url.password) return null;
    return `${url.origin}${url.pathname}`;
  } catch {
    return null;
  }
}

export function Cameras() {
  const t = useT();
  const search = useSearch({ strict: false }) as { site_id?: string; server_id?: string };
  const [filter, setFilter] = useState<CameraFilter>({ site_id: search.site_id, server_id: search.server_id });
  const [discoveryServerId, setDiscoveryServerId] = useState("");
  const [interfaceName, setInterfaceName] = useState("");
  const [discovery, setDiscovery] = useState<{ pending: boolean; error?: unknown; devices?: { xaddrs: string[] }[] }>({ pending: false });
  const discoveryVersion = useRef(0);
  const activeDiscovery = useRef<AbortController | null>(null);
  const cameras = useQuery(camerasQuery(filter));
  const sites = useQuery(sitesQuery);
  const servers = useQuery(serversQuery);
  const me = useQuery(meQuery);
  const policy = useQuery(classifyPolicyQuery);
  const cameraOn = new Map(policy.data?.cameras.map((c) => [c.id, c]));
  const siteName = new Map(sites.data?.map((s) => [s.id, s.name]));
  const serverName = new Map(servers.data?.map((s) => [s.id, s.name]));
  const update = (k: keyof CameraFilter, v: string) => setFilter((f) => ({ ...f, [k]: v || undefined }));
  const selectedDiscoveryServer = servers.data?.find((server) => server.id === discoveryServerId);
  const canDiscover = !!selectedDiscoveryServer && canManageServer(me.data, selectedDiscoveryServer) && interfaceName.trim().length > 0 && !discovery.pending;
  const cancelDiscovery = () => {
    discoveryVersion.current += 1;
    activeDiscovery.current?.abort();
    activeDiscovery.current = null;
    setDiscovery({ pending: false });
  };
  useEffect(() => () => {
    discoveryVersion.current += 1;
    activeDiscovery.current?.abort();
    activeDiscovery.current = null;
  }, []);
  const selectDiscoveryServer = (serverId: string) => {
    cancelDiscovery();
    setDiscoveryServerId(serverId);
  };
  const changeInterface = (value: string) => {
    cancelDiscovery();
    setInterfaceName(value);
  };
  const searchOnvif = async () => {
    if (!selectedDiscoveryServer || !canManageServer(me.data, selectedDiscoveryServer) || !interfaceName.trim()) return;
    const version = ++discoveryVersion.current;
    const serverId = selectedDiscoveryServer.id;
    const requestedInterface = interfaceName.trim();
    const controller = new AbortController();
    activeDiscovery.current = controller;
    setDiscovery({ pending: true });
    try {
      const result = unwrap(await api.POST("/api/v1/servers/{serverId}/onvif/discover", {
        params: { path: { serverId } },
        body: { interface_name: requestedInterface },
        signal: controller.signal,
      }));
      if (version === discoveryVersion.current && serverId === discoveryServerId && requestedInterface === interfaceName.trim()) setDiscovery({ pending: false, devices: result.devices });
    } catch (error) {
      if (!controller.signal.aborted && version === discoveryVersion.current && serverId === discoveryServerId && requestedInterface === interfaceName.trim()) setDiscovery({ pending: false, error });
    } finally {
      if (activeDiscovery.current === controller) activeDiscovery.current = null;
    }
  };

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader title={t("nav.cameras")} description={t("settings.camerasOnly")} />
      <section aria-labelledby="onvif-discovery-title" className="flex flex-col gap-3 rounded-m3-xl bg-surface-1 p-4">
        <div>
          <h2 id="onvif-discovery-title" className="text-lg font-semibold">Descubrir cámaras ONVIF</h2>
          <p className="text-sm text-on-surface-variant">La búsqueda se ejecuta desde el agente del servidor. No se envían credenciales y los dispositivos encontrados todavía no se agregaron.</p>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="flex flex-col gap-1 text-sm font-medium">Servidor para descubrir cámaras
            <Select aria-label="Servidor para descubrir cámaras" value={discoveryServerId} onChange={(event) => selectDiscoveryServer(event.target.value)}>
              <option value="">Seleccione un servidor</option>
              {servers.data?.map((server) => <option key={server.id} value={server.id}>{server.name}</option>)}
            </Select>
          </label>
          {(!selectedDiscoveryServer || canManageServer(me.data, selectedDiscoveryServer)) && <label className="flex flex-col gap-1 text-sm font-medium">Interfaz de red del agente
            <TextInput aria-label="Interfaz de red del agente" value={interfaceName} onChange={(event) => changeInterface(event.target.value)} placeholder="Por ejemplo: eth0" />
          </label>}
        </div>
        {selectedDiscoveryServer && !canManageServer(me.data, selectedDiscoveryServer) && <p className="text-sm text-muted">No tiene permiso servers.manage para este servidor.</p>}
        {(!selectedDiscoveryServer || canManageServer(me.data, selectedDiscoveryServer)) && <div><button type="button" className="h-11 rounded-m3-full bg-primary px-5 text-sm font-medium text-on-primary disabled:cursor-not-allowed disabled:opacity-50" disabled={!canDiscover} onClick={() => void searchOnvif()}>{discovery.pending ? "Buscando…" : "Buscar dispositivos ONVIF"}</button></div>}
        <ErrorNote error={discovery.error} />
        {discovery.pending && <p role="status" className="text-sm text-muted">Buscando dispositivos en la red del servidor…</p>}
        {discovery.devices?.length === 0 && <p role="status" className="text-sm text-muted">No se encontraron dispositivos ONVIF en esa interfaz.</p>}
        {!!discovery.devices?.length && <ul aria-label="Dispositivos ONVIF encontrados" className="flex flex-col gap-2">
          {discovery.devices.map((device, index) => <li key={`${index}-${device.xaddrs.join("|")}`} className="rounded-m3-lg bg-surface-2 p-3">
            <p className="text-sm font-medium">Dispositivo encontrado · todavía no se agregó</p>
            {device.xaddrs.map((xaddr) => <code key={xaddr} className="block break-all text-xs text-on-surface-variant">{displayEndpoint(xaddr) ?? "Endpoint no disponible"}</code>)}
          </li>)}
        </ul>}
      </section>
      <div className="grid gap-3 rounded-m3-xl bg-surface-1 p-4 sm:grid-cols-3">
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
      {cameras.data && <Summary>{summarize(cameras.data)}</Summary>}
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
              <Th>Clasificación</Th>
              <Th className="text-right">Ajustes</Th>
            </tr>
          </thead>
          <tbody>
            {cameras.data.map((c) => (
              <tr key={c.id} className="border-t border-outline-variant align-top">
                <td>
                  <div className="flex items-center gap-2 font-medium">
                    {c.display_name}
                    {c.lpr && <span className="rounded-m3-sm bg-secondary-container px-1.5 py-0.5 font-mono text-[10px] text-on-secondary-container">LPR</span>}
                  </div>
                  {c.display_name !== c.remote_name && <div className="font-mono text-xs text-muted">{c.remote_name}</div>}
                  {c.missing_since && <div role="status" className="text-xs text-warn">Ya no aparece en Frigate</div>}
                </td>
                <td>{siteName.get(c.site_id) ?? "—"}</td>
                <td>
                  {serverName.has(c.server_id) ? (
                    <Link to="/servers" search={{ site_id: c.site_id }} className="hover:underline">
                      {serverName.get(c.server_id)}
                    </Link>
                  ) : (
                    "—"
                  )}
                </td>
                <td>{c.enabled ? <StatusBadge status={c.status} /> : <span className="text-xs text-muted">Deshabilitada</span>}</td>
                <td className="text-right font-mono text-xs tabular-nums">{c.fps != null ? c.fps.toFixed(1) : "—"}</td>
                <td className="text-xs text-muted">{c.zones.join(", ") || "—"}</td>
                <td>
                  <BodyClassifySwitch
                    scope="camera"
                    id={c.id}
                    enabled={cameraOn.get(c.id)?.enabled ?? true}
                    disabled={!can(me.data, "cameras.manage")}
                    label={cameraOn.get(c.id)?.effective === false && (cameraOn.get(c.id)?.enabled ?? true) ? "Apagada en el servidor" : "Clasificación fina"}
                  />
                </td>
                <td className="text-right">
                  <LinkButton
                    variant="tonal"
                    to="/cameras/$cameraId/frigate"
                    params={{ cameraId: c.id }}
                    aria-label={`Configuración de ${c.display_name}`}
                    title={t("nav.settings")}
                    className="size-11 px-0"
                  >
                    <Icon icon={Settings} size="sm" />
                  </LinkButton>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}
