import { useT } from "@/i18n";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Sparkles } from "lucide-react";
import { useMemo, useState } from "react";
import { type CameraFilter, camerasQuery, classifyPolicyQuery, meQuery, serversQuery, sitesQuery } from "@/api/queries";
import { BulkConfigureDialog } from "@/components/camera/bulk/BulkConfigureDialog";
import { DeletedCameras } from "@/components/camera/DeletedCameras";
import { DeleteCameraDialog } from "@/components/camera/MoreActions";
import { CameraTree } from "@/components/cameras/CameraTree";
import { FilterRail } from "@/components/cameras/FilterRail";
import { can, canCamera } from "@/lib/perm";
import { type AgentFilter, type CameraStatusFilter, agentInfo, buildCameraTree, isVirtualCamera, latestAgentVersion } from "@/lib/cameraTree";
import { Icon } from "@/components/Icon";
import { Button, Empty, ErrorNote, PageHeader, Summary } from "@/components/ui";
import { IaViewModal } from "@/components/IaViewModal";
/** Most cameras one bulk configuration may target from the list selection. */
const BULK_CAMERA_CAP = 200;

function summarize(cameras: { enabled: boolean; status: string; onvif_profile?: string | null }[]) {
  const online = cameras.filter((c) => c.enabled && c.status === "online").length;
  const onvifLinked = cameras.filter((c) => Boolean(c.onvif_profile)).length;
  const disabled = cameras.filter((c) => !c.enabled).length;
  const parts = [`${cameras.length} ${cameras.length === 1 ? "cámara" : "cámaras"}`, `${online} en línea`];
  if (onvifLinked > 0) parts.push(`${onvifLinked} con ONVIF`);
  if (disabled) parts.push(`${disabled} ${disabled === 1 ? "deshabilitada" : "deshabilitadas"}`);
  return parts.join(" · ");
}

export function Cameras() {
  const t = useT();
  const search = useSearch({ strict: false }) as { site_id?: string; server_id?: string; q?: string };
  const [filter, setFilter] = useState<CameraFilter>({ site_id: search.site_id, server_id: search.server_id, q: search.q });
  const [statuses, setStatuses] = useState<CameraStatusFilter[]>([]);
  const [agents, setAgents] = useState<AgentFilter[]>([]);
  const [showVirtual, setShowVirtual] = useState(false);
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const [iaViewOpen, setIaViewOpen] = useState(false);
  // The search runs here, over name, remote name and server, so it is not sent to the API.
  const cameras = useQuery(camerasQuery({ site_id: filter.site_id, server_id: filter.server_id }));
  const sites = useQuery(sitesQuery);
  const servers = useQuery(serversQuery);
  const me = useQuery(meQuery);
  const policy = useQuery(classifyPolicyQuery);
  const cameraOn = new Map(policy.data?.cameras.map((c) => [c.id, c]));
  const [deletingCamera, setDeletingCamera] = useState<{ id: string; name: string } | null>(null);
  const navigate = useNavigate();
  const [picked, setPicked] = useState<string[]>([]);
  const [bulkOpen, setBulkOpen] = useState(false);
  const canBulk = canCamera(me.data, "cameras.config.edit");
  const togglePicked = (id: string) => setPicked((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : cur.length >= BULK_CAMERA_CAP ? cur : [...cur, id]));

  const update = (k: keyof CameraFilter, v: string) => {
    setFilter((f) => ({ ...f, [k]: v || undefined }));
    void navigate({
      to: ".",
      search: ((prev: Record<string, unknown>) => {
        const next = { ...prev };
        if (v) next[k] = v;
        else delete next[k];
        return next;
      }) as never,
    });
  };

  const [prevSearch, setPrevSearch] = useState({ site_id: search.site_id, server_id: search.server_id });
  if (prevSearch.site_id !== search.site_id || prevSearch.server_id !== search.server_id) {
    setPrevSearch({ site_id: search.site_id, server_id: search.server_id });
    setFilter((f) => ({ ...f, site_id: search.site_id, server_id: search.server_id }));
  }

  const allServers = useMemo(() => (servers.data ?? []).filter((s) => !filter.site_id || s.site_id === filter.site_id), [servers.data, filter.site_id]);
  const latest = latestAgentVersion(servers.data ?? []);
  const q = filter.q ?? "";
  const tree = useMemo(
    () => buildCameraTree(cameras.data ?? [], allServers, sites.data ?? [], { q, statuses, agents, showVirtual }),
    [cameras.data, allServers, sites.data, q, statuses, agents, showVirtual],
  );
  const shown = tree.flatMap((s) => s.servers.flatMap((n) => n.cameras));
  const virtualCount = (cameras.data ?? []).filter(isVirtualCamera).length;
  // Per-server counts for the rail: the cameras already loaded, else what the server reports.
  const railServers = allServers.map((server) => {
    const loaded = (cameras.data ?? []).filter((c) => c.server_id === server.id && (showVirtual || !isVirtualCamera(c)));
    return { server, count: cameras.data && (!filter.server_id || filter.server_id === server.id) ? loaded.length : server.camera_count, agent: agentInfo(server, latest) };
  });
  const toggleServer = (id: string) =>
    setCollapsed((cur) => {
      const next = new Set(cur);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  const allCollapsed = tree.length > 0 && tree.every((s) => s.servers.every((n) => collapsed.has(n.server.id)));
  const toggleAll = () => setCollapsed(allCollapsed ? new Set() : new Set(tree.flatMap((s) => s.servers.map((n) => n.server.id))));

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader
        title={t("nav.cameras")}
        description={t("settings.camerasOnly")}
        actions={
          <button
            type="button"
            className="flex h-11 items-center gap-1.5 rounded-m3-full border border-primary/40 bg-primary/10 px-4 text-sm font-semibold text-primary hover:bg-primary/18"
            onClick={() => setIaViewOpen(true)}
          >
            <Icon icon={Sparkles} size="xs" />
            Canal IA-View
          </button>
        }
      />
      <div className="@container flex flex-col gap-6">
        <div className="flex flex-col gap-4 @3xl:flex-row @3xl:items-start">
          <FilterRail
            q={q}
            onQ={(v) => update("q", v)}
            siteId={filter.site_id ?? ""}
            onSite={(v) => update("site_id", v)}
            sites={sites.data ?? []}
            serverId={filter.server_id ?? ""}
            onServer={(v) => update("server_id", v)}
            servers={railServers}
            statuses={statuses}
            onStatuses={setStatuses}
            agents={agents}
            onAgents={setAgents}
            showVirtual={showVirtual}
            onShowVirtual={setShowVirtual}
            virtualCount={virtualCount}
          />
          <div className="flex min-w-0 flex-1 flex-col gap-3">
            {filter.server_id && can(me.data, "servers.manage") && <DeletedCameras serverId={filter.server_id} />}
            <ErrorNote error={cameras.error} />
            <div className="flex flex-wrap items-center justify-between gap-2">
              {cameras.data ? <Summary>{summarize(shown)}</Summary> : <span />}
              {!filter.server_id && tree.length > 0 && (
                <Button variant="text" size="sm" onClick={toggleAll}>
                  {allCollapsed ? "Expandir todo" : "Contraer todo"}
                </Button>
              )}
            </div>
            {cameras.data && shown.length === 0 && <Empty>No hay cámaras que coincidan.</Empty>}
            {canBulk && picked.length > 0 && (
              <div role="region" aria-label="Selección de cámaras" className="flex flex-wrap items-center gap-3 rounded-m3-lg bg-surface-2 px-4 py-2 text-sm">
                <Button variant="filled" onClick={() => setBulkOpen(true)}>Configurar cámaras ({picked.length})</Button>
                <Button variant="text" onClick={() => setPicked([])}>Limpiar selección</Button>
                {picked.length >= BULK_CAMERA_CAP && <span role="status" className="text-xs text-warn">Máximo {BULK_CAMERA_CAP} cámaras por cambio masivo.</span>}
              </div>
            )}
            {bulkOpen && <BulkConfigureDialog target={{ cameraIds: picked }} targetLabel={`${picked.length} cámaras`} onClose={() => setBulkOpen(false)} onDone={() => setPicked([])} />}
            {!!cameras.data && tree.length > 0 && (
              <CameraTree
                tree={tree}
                flat={!!filter.server_id}
                collapsed={collapsed}
                forceOpen={q.trim() !== ""}
                onToggle={toggleServer}
                picked={picked}
                onPick={togglePicked}
                analyticsOf={(id) => cameraOn.get(id)}
                selectable={canBulk}
                canManageCameras={can(me.data, "cameras.manage")}
                canDelete={can(me.data, "servers.manage")}
                onDelete={(id, name) => setDeletingCamera({ id, name })}
              />
            )}
          </div>
        </div>
      </div>
      {deletingCamera && (
        <DeleteCameraDialog
          cameraId={deletingCamera.id}
          name={deletingCamera.name}
          recorder
          onClose={() => setDeletingCamera(null)}
          onDeleted={() => setDeletingCamera(null)}
        />
      )}
      <IaViewModal open={iaViewOpen} onClose={() => setIaViewOpen(false)} />
    </div>
  );
}
