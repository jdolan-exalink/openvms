import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { Check, Settings, Sparkles } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { type CameraFilter, camerasQuery, classifyPolicyQuery, meQuery, serverAgentQuery, serversQuery, sitesQuery } from "@/api/queries";
import { api, unwrap, ApiError } from "@/api/client";
import { BodyClassifySwitch } from "@/components/BodyClassifySwitch";
import { BulkConfigureDialog } from "@/components/camera/bulk/BulkConfigureDialog";
import { DeletedCameras } from "@/components/camera/DeletedCameras";
import { DeleteCameraDialog } from "@/components/camera/MoreActions";
import { can, canCamera } from "@/lib/perm";
import { Icon } from "@/components/Icon";
import { Button, Empty, ErrorNote, LinkButton, PageHeader, Select, StatusBadge, Summary, Table, TextInput, Th } from "@/components/ui";
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

function hasServerPermission(me: { tenant_id: string | null; grants: { permission: string; effect: string; scope_type: string; scope_id?: string }[] } | undefined, permission: string, server: { id: string; site_id: string }) {
  if (!me) return false;
  const applies = (grant: (typeof me.grants)[number]) => {
    if (grant.permission !== permission) return false;
    if (grant.scope_type === "platform") return true;
    if (grant.scope_type === "tenant") return grant.scope_id === me.tenant_id;
    if (grant.scope_type === "site") return grant.scope_id === server.site_id;
    if (grant.scope_type === "server") return grant.scope_id === server.id;
    return false;
  };
  const grants = me.grants.filter(applies);
  return grants.some((grant) => grant.effect === "allow") && !grants.some((grant) => grant.effect === "deny");
}

function isLiteralProbeEndpoint(value: string) {
  try {
    const url = new URL(value);
    return (url.protocol === "http:" || url.protocol === "https:") &&
      /^(?:\d{1,3}\.){3}\d{1,3}$/.test(url.hostname) &&
      url.hostname.split(".").every((part) => Number(part) <= 255) &&
      !url.username && !url.password && !url.search && !url.hash;
  } catch {
    return false;
  }
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

export function isUsableDiscoveryInterface(name: string): boolean {
  const normalized = name.trim().toLowerCase();
  if (!normalized) return false;
  if (normalized === "lo" || normalized.startsWith("loopback")) return false;
  if (normalized.startsWith("docker") || normalized.startsWith("br-") || normalized.startsWith("veth")) return false;
  return true;
}

export function formatDiscoveryError(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "discovery_unsupported" || error.status === 409) {
      return "El agente del servidor tiene el descubrimiento deshabilitado o la interfaz no está permitida. Requiere configurar ONVIF_DISCOVERY_INTERFACES y ONVIF_ALLOWED_CIDRS en el agente.";
    }
  }
  return error instanceof Error ? error.message : String(error);
}

export function Cameras() {
  const t = useT();
  const search = useSearch({ strict: false }) as { site_id?: string; server_id?: string };
  const [filter, setFilter] = useState<CameraFilter>({ site_id: search.site_id, server_id: search.server_id });
  const [discoveryServerId, setDiscoveryServerId] = useState("");
  const [selectedInterfaceName, setSelectedInterfaceName] = useState<string | null>(null);
  const [customInterface, setCustomInterface] = useState(false);
  const [discovery, setDiscovery] = useState<{ pending: boolean; error?: unknown; devices?: { xaddrs: string[] }[] }>({ pending: false });
  const [probeServerId, setProbeServerId] = useState("");
  const [probeEndpoint, setProbeEndpoint] = useState("");
  const [probeUsername, setProbeUsername] = useState("");
  const [probePassword, setProbePassword] = useState("");
  const [probe, setProbe] = useState<{ pending: boolean; result?: import("@/api/client").Schemas["OnvifProbeResult"]; error?: string }>({ pending: false });
  const [iaViewOpen, setIaViewOpen] = useState(false);
  const discoveryVersion = useRef(0);
  const activeDiscovery = useRef<AbortController | null>(null);
  const probeVersion = useRef(0);
  const activeProbe = useRef<AbortController | null>(null);
  const cameras = useQuery(camerasQuery(filter));
  const sites = useQuery(sitesQuery);
  const servers = useQuery(serversQuery);
  const me = useQuery(meQuery);
  const policy = useQuery(classifyPolicyQuery);
  const agent = useQuery(serverAgentQuery(discoveryServerId));
  const serverInterfaces = agent.data?.network_interfaces ?? [];
  const filteredInterfaces = Array.from(
    new Set(serverInterfaces.map((i) => i.name.trim()).filter(isUsableDiscoveryInterface))
  ).sort();
  const cameraOn = new Map(policy.data?.cameras.map((c) => [c.id, c]));
  const siteName = new Map(sites.data?.map((s) => [s.id, s.name]));
  const serverName = new Map(servers.data?.map((s) => [s.id, s.name]));
  const recorderIds = new Set(servers.data?.filter((s) => s.kind === "provision_nvr").map((s) => s.id));
  const [deletingCamera, setDeletingCamera] = useState<{ id: string; name: string } | null>(null);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [checkResult, setCheckResult] = useState<string | null>(null);
  const [picked, setPicked] = useState<string[]>([]);
  const [bulkOpen, setBulkOpen] = useState(false);
  const canBulk = canCamera(me.data, "cameras.config.edit");
  const togglePicked = (id: string) => setPicked((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : cur.length >= BULK_CAMERA_CAP ? cur : [...cur, id]));

  const [addingDevice, setAddingDevice] = useState<{
    xaddr: string;
    host: string;
    name: string;
    mainUrl: string;
    subUrl: string;
  } | null>(null);

  const addCamera = useMutation({
    mutationFn: async (vars: { name: string; mainUrl: string; subUrl?: string; onvifHost?: string }) => {
      if (!discoveryServerId) throw new Error("Servidor no seleccionado");
      return unwrap(
        await api.POST("/api/v1/servers/{serverId}/frigate/add-camera", {
          params: { path: { serverId: discoveryServerId } },
          body: {
            name: vars.name,
            main_stream_url: vars.mainUrl,
            sub_stream_url: vars.subUrl || undefined,
            onvif_host: vars.onvifHost || undefined,
          },
        })
      );
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["cameras"] });
      setAddingDevice(null);
    },
  });

  const [linkingCameraFor, setLinkingCameraFor] = useState<{ xaddr: string; selectedCameraId: string } | null>(null);

  const checkCamera = useMutation({
    mutationFn: async ({ cameraId, endpoint }: { cameraId: string; endpoint: string }) => {
      return unwrap(
        await api.POST("/api/v1/cameras/{cameraId}/onvif/check", {
          params: { path: { cameraId } },
          body: { endpoint: endpoint || undefined },
        })
      );
    },
    onSuccess: async (res) => {
      setCheckResult(`Cámara "${res.display_name}" vinculada con ONVIF (${res.onvif_profile ?? "OK"}).`);
      setLinkingCameraFor(null);
      await qc.invalidateQueries({ queryKey: ["cameras"] });
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : String(err);
      setCheckResult(`Error al vincular con ONVIF: ${msg}`);
    },
  });

  const matchExistingCamera = (deviceXaddrs: string[]) => {
    if (!cameras.data) return null;

    return cameras.data.find((c) => {
      if (discoveryServerId && c.server_id !== discoveryServerId) return false;
      return Boolean(c.onvif_endpoint && deviceXaddrs.includes(c.onvif_endpoint));
    });
  };

  const unlinkedCameras =
    cameras.data?.filter(
      (c) => (!discoveryServerId || c.server_id === discoveryServerId) && !c.onvif_profile
    ) ?? [];

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

  const selectedDiscoveryServer = servers.data?.find((server) => server.id === discoveryServerId);
  const defaultInterface = filteredInterfaces.includes("eth0")
    ? "eth0"
    : (filteredInterfaces[0] ?? (selectedDiscoveryServer ? "eth0" : ""));
  const interfaceName = selectedInterfaceName ?? defaultInterface;
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
    probeVersion.current += 1;
    activeProbe.current?.abort();
    activeProbe.current = null;
  }, []);
  const cancelProbe = () => {
    probeVersion.current += 1;
    activeProbe.current?.abort();
    activeProbe.current = null;
    setProbePassword("");
    setProbeUsername("");
    setProbe({ pending: false });
  };
  const selectedProbeServer = servers.data?.find((server) => server.id === probeServerId);
  const canProbeServer = !!selectedProbeServer &&
    hasServerPermission(me.data, "servers.manage", selectedProbeServer) &&
    hasServerPermission(me.data, "servers.config.secrets", selectedProbeServer);
  const credentialPairValid = Boolean(probeUsername) === Boolean(probePassword);
  const canProbe = canProbeServer && isLiteralProbeEndpoint(probeEndpoint) && credentialPairValid && !probe.pending;
  const probeOnvif = async () => {
    if (!selectedProbeServer || !canProbeServer || !isLiteralProbeEndpoint(probeEndpoint)) return;
    const version = ++probeVersion.current;
    const serverId = selectedProbeServer.id;
    const endpoint = probeEndpoint.trim();
    const controller = new AbortController();
    activeProbe.current = controller;
    setProbe({ pending: true });
    setProbePassword("");
    try {
      const result = unwrap(await api.POST("/api/v1/servers/{serverId}/onvif/probe", {
        params: { path: { serverId } },
        body: { endpoint, ...(probeUsername ? { username: probeUsername } : {}), ...(probePassword ? { password: probePassword } : {}) },
        signal: controller.signal,
      }));
      if (version === probeVersion.current && serverId === probeServerId && endpoint === probeEndpoint.trim()) setProbe({ pending: false, result });
    } catch (error) {
      if (!controller.signal.aborted && version === probeVersion.current && serverId === probeServerId && endpoint === probeEndpoint.trim()) {
        const code = typeof error === "object" && error !== null && "code" in error ? String(error.code) : "";
        setProbe({ pending: false, error: code === "agent_tls_not_configured" ? "El servidor todavía no tiene configurada la confianza TLS para el agente. Pida a una persona administradora que la configure." : "No se pudo probar el dispositivo ONVIF. Revise el endpoint, la conectividad y la confianza TLS configurada." });
      }
    } finally {
      if (version === probeVersion.current && activeProbe.current === controller) {
        activeProbe.current = null;
        setProbePassword("");
        setProbeUsername("");
      }
    }
  };
  const selectDiscoveryServer = (serverId: string) => {
    cancelDiscovery();
    setDiscoveryServerId(serverId);
    setSelectedInterfaceName(null);
    setCustomInterface(false);
  };
  const changeInterface = (value: string) => {
    cancelDiscovery();
    setSelectedInterfaceName(value);
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
          <p className="text-sm text-on-surface-variant">
            La búsqueda se ejecuta desde el agente del servidor en su red física. Si las cámaras ya existen en Frigate, usá <strong>Comprobar y vincular ONVIF en cámaras</strong> para enlazarlas automáticamente. Los dispositivos no registrados pueden agregarse como nuevas cámaras o vincularse manualmente.
          </p>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="flex flex-col gap-1 text-sm font-medium">Servidor para descubrir cámaras
            <Select aria-label="Servidor para descubrir cámaras" value={discoveryServerId} onChange={(event) => selectDiscoveryServer(event.target.value)}>
              <option value="">Seleccione un servidor</option>
              {servers.data?.map((server) => <option key={server.id} value={server.id}>{server.name}</option>)}
            </Select>
          </label>
          {(!selectedDiscoveryServer || canManageServer(me.data, selectedDiscoveryServer)) && (
            <div className="flex flex-col gap-1 text-sm font-medium">
              <div className="flex items-center justify-between">
                <label htmlFor="discovery-interface">Interfaz de red del agente</label>
                {filteredInterfaces.length > 0 && (
                  <button
                    type="button"
                    className="text-xs font-normal text-primary hover:underline"
                    onClick={() => {
                      const next = !customInterface;
                      setCustomInterface(next);
                      setSelectedInterfaceName(next ? (interfaceName || "eth0") : null);
                    }}
                  >
                    {customInterface ? "Elegir de la lista" : "Ingresar manualmente"}
                  </button>
                )}
              </div>
              {filteredInterfaces.length > 0 && !customInterface ? (
                <Select
                  id="discovery-interface"
                  aria-label="Interfaz de red del agente"
                  value={interfaceName}
                  onChange={(event) => {
                    const val = event.target.value;
                    if (val === "__custom__") {
                      setCustomInterface(true);
                      setSelectedInterfaceName(interfaceName || "eth0");
                    } else {
                      changeInterface(val);
                    }
                  }}
                >
                  <option value="">Seleccione una interfaz</option>
                  <option value="*">Todas las interfaces (*)</option>
                  {filteredInterfaces.map((name) => (
                    <option key={name} value={name}>
                      {name}
                    </option>
                  ))}
                  <option value="__custom__">Ingresar manualmente…</option>
                </Select>
              ) : (
                <TextInput
                  id="discovery-interface"
                  aria-label="Interfaz de red del agente"
                  value={interfaceName}
                  onChange={(event) => changeInterface(event.target.value)}
                  placeholder="Por ejemplo: eth0"
                />
              )}
            </div>
          )}
        </div>
        {selectedDiscoveryServer && !canManageServer(me.data, selectedDiscoveryServer) && <p className="text-sm text-muted">No tiene permiso servers.manage para este servidor.</p>}
        {(!selectedDiscoveryServer || canManageServer(me.data, selectedDiscoveryServer)) && (
          <div className="flex flex-wrap items-center gap-2">
            <button type="button" className="h-11 rounded-m3-full bg-primary px-5 text-sm font-medium text-on-primary disabled:cursor-not-allowed disabled:opacity-50" disabled={!canDiscover} onClick={() => void searchOnvif()}>{discovery.pending ? "Buscando…" : "Buscar dispositivos ONVIF"}</button>
            <button
              type="button"
              className="flex h-11 items-center gap-1.5 rounded-m3-full border border-primary/40 bg-primary/10 px-4 text-sm font-semibold text-primary hover:bg-primary/18"
              onClick={() => setIaViewOpen(true)}
            >
              <Icon icon={Sparkles} size="xs" />
              Canal IA-View
            </button>
          </div>
        )}
        {checkResult && <p role="status" className="rounded-m3-lg bg-ok/12 px-4 py-2.5 text-sm text-ok">{checkResult}</p>}
        <ErrorNote error={formatDiscoveryError(discovery.error)} />
        {discovery.pending && <p role="status" className="text-sm text-muted">Buscando dispositivos en la red del servidor…</p>}
        {discovery.devices?.length === 0 && <p role="status" className="text-sm text-muted">No se encontraron dispositivos ONVIF en esa interfaz.</p>}
        {!!discovery.devices?.length && (
          <ul aria-label="Dispositivos ONVIF encontrados" className="flex flex-col gap-2">
            {discovery.devices.map((device, index) => {
              const primaryEndpoint = device.xaddrs[0] || "";
              const host = (() => {
                try {
                  return new URL(primaryEndpoint).hostname;
                } catch {
                  return "";
                }
              })();
              const isAddingThis = addingDevice?.xaddr === primaryEndpoint;
              const isLinkingThis = linkingCameraFor?.xaddr === primaryEndpoint;
              const matchedCam = matchExistingCamera(device.xaddrs);

              return (
                <li key={`${index}-${device.xaddrs.join("|")}`} className="flex flex-col gap-2 rounded-m3-lg bg-surface-2 p-3">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div>
                      {matchedCam ? (
                        matchedCam.onvif_profile ? (
                          <div className="flex items-center gap-2">
                            <p className="flex items-center gap-1 text-sm font-semibold text-ok">
                              <Icon icon={Check} size="xs" />
                              Ya vinculada con {matchedCam.display_name}
                            </p>
                            <span className="rounded-m3-sm bg-primary/12 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-primary">
                              {matchedCam.onvif_profile}
                            </span>
                          </div>
                        ) : (
                          <div>
                            <p className="text-sm font-semibold text-primary">
                              Cámara en inventario: {matchedCam.display_name}
                            </p>
                            <p className="text-xs text-muted">
                              Detectada en Frigate · Lista para vincular ONVIF
                            </p>
                          </div>
                        )
                      ) : (
                        <p className="text-sm font-medium">Dispositivo encontrado · todavía no se agregó</p>
                      )}
                      {device.xaddrs.map((xaddr) => (
                        <code key={xaddr} className="block break-all text-xs text-on-surface-variant">
                          {displayEndpoint(xaddr) ?? "Endpoint no disponible"}
                        </code>
                      ))}
                    </div>
                    <div className="flex items-center gap-2">
                      <button
                        type="button"
                        className="rounded-m3-full border border-outline-variant px-3 py-1 text-xs font-medium hover:bg-surface-3"
                        onClick={() => {
                          setProbeServerId(discoveryServerId);
                          setProbeEndpoint(primaryEndpoint);
                        }}
                      >
                        Probar
                      </button>

                      {matchedCam ? (
                        <button
                          type="button"
                          className="rounded-m3-full bg-primary px-3 py-1 text-xs font-medium text-on-primary hover:bg-primary/90 disabled:opacity-50"
                          disabled={checkCamera.isPending}
                          onClick={() => checkCamera.mutate({ cameraId: matchedCam.id, endpoint: primaryEndpoint })}
                        >
                          {checkCamera.isPending ? "Vinculando…" : matchedCam.onvif_profile ? "Recomprobar" : "Vincular ONVIF"}
                        </button>
                      ) : (
                        <>
                          {unlinkedCameras.length > 0 && (
                            <button
                              type="button"
                              className="rounded-m3-full border border-primary/40 bg-primary/10 px-3 py-1 text-xs font-medium text-primary hover:bg-primary/20"
                              onClick={() => {
                                setAddingDevice(null);
                                setLinkingCameraFor(isLinkingThis ? null : { xaddr: primaryEndpoint, selectedCameraId: unlinkedCameras[0]?.id || "" });
                              }}
                            >
                              {isLinkingThis ? "Cancelar" : "Vincular a cámara existente"}
                            </button>
                          )}
                          <button
                            type="button"
                            className="rounded-m3-full bg-primary px-3 py-1 text-xs font-medium text-on-primary hover:bg-primary/90"
                            onClick={() => {
                              setLinkingCameraFor(null);
                              const safeHost = host.replace(/[^a-zA-Z0-9]/g, "_");
                              setAddingDevice({
                                xaddr: primaryEndpoint,
                                host,
                                name: `cam_${safeHost}`,
                                mainUrl: `rtsp://${host}:554/live/ch0`,
                                subUrl: "",
                              });
                            }}
                          >
                            Agregar a Frigate
                          </button>
                        </>
                      )}
                    </div>
                  </div>

                  {isLinkingThis && (
                    <div className="mt-2 flex flex-wrap items-center gap-2 rounded-m3-md border border-outline-variant bg-surface-1 p-3">
                      <span className="text-xs font-medium">Seleccionar cámara a vincular:</span>
                      <Select
                        aria-label="Cámara existente para vincular ONVIF"
                        value={linkingCameraFor.selectedCameraId}
                        onChange={(e) => setLinkingCameraFor({ ...linkingCameraFor, selectedCameraId: e.target.value })}
                        className="text-xs"
                      >
                        {unlinkedCameras.map((c) => (
                          <option key={c.id} value={c.id}>
                            {c.display_name} ({c.remote_name})
                          </option>
                        ))}
                      </Select>
                      <button
                        type="button"
                        className="rounded-m3-full bg-primary px-3 py-1 text-xs font-medium text-on-primary hover:bg-primary/90 disabled:opacity-50"
                        disabled={checkCamera.isPending || !linkingCameraFor.selectedCameraId}
                        onClick={() => {
                          checkCamera.mutate({ cameraId: linkingCameraFor.selectedCameraId, endpoint: primaryEndpoint });
                        }}
                      >
                        {checkCamera.isPending ? "Vinculando…" : "Confirmar vinculación"}
                      </button>
                    </div>
                  )}

                  {isAddingThis && (
                    <form
                      onSubmit={(e) => {
                        e.preventDefault();
                        if (addingDevice) {
                          addCamera.mutate({
                            name: addingDevice.name,
                            mainUrl: addingDevice.mainUrl,
                            subUrl: addingDevice.subUrl,
                            onvifHost: addingDevice.host,
                          });
                        }
                      }}
                      className="mt-2 flex flex-col gap-2 rounded-m3-md border border-outline-variant bg-surface-1 p-3"
                    >
                      <p className="text-xs font-semibold text-ink">Agregar cámara a Frigate y sincronizar:</p>
                      <div className="grid gap-2 sm:grid-cols-3">
                        <label className="flex flex-col gap-1 text-xs font-medium">
                          Nombre (letras, números, _)
                          <TextInput
                            value={addingDevice.name}
                            onChange={(e) => setAddingDevice({ ...addingDevice, name: e.target.value })}
                            required
                          />
                        </label>
                        <label className="flex flex-col gap-1 text-xs font-medium">
                          RTSP Stream Principal
                          <TextInput
                            value={addingDevice.mainUrl}
                            onChange={(e) => setAddingDevice({ ...addingDevice, mainUrl: e.target.value })}
                            required
                          />
                        </label>
                        <label className="flex flex-col gap-1 text-xs font-medium">
                          RTSP Substream (opcional)
                          <TextInput
                            value={addingDevice.subUrl}
                            onChange={(e) => setAddingDevice({ ...addingDevice, subUrl: e.target.value })}
                            placeholder="Opcional para detección liviana"
                          />
                        </label>
                      </div>
                      <ErrorNote error={addCamera.error} />
                      <div className="flex items-center justify-end gap-2 pt-1">
                        <button
                          type="button"
                          className="rounded-m3-full px-3 py-1 text-xs font-medium text-muted hover:bg-surface-3"
                          onClick={() => setAddingDevice(null)}
                        >
                          Cancelar
                        </button>
                        <button
                          type="submit"
                          disabled={addCamera.isPending}
                          className="rounded-m3-full bg-primary px-4 py-1.5 text-xs font-medium text-on-primary disabled:opacity-50"
                        >
                          {addCamera.isPending ? "Guardando en Frigate…" : "Guardar y sincronizar"}
                        </button>
                      </div>
                    </form>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </section>
      <section aria-labelledby="onvif-probe-title" className="flex flex-col gap-3 rounded-m3-xl bg-surface-1 p-4">
        <div>
          <h2 id="onvif-probe-title" className="text-lg font-semibold">Probar y detectar</h2>
          <p className="text-sm text-on-surface-variant">Consulta información de solo lectura. Las credenciales se envían de forma transitoria y no se guardan. El resultado no agrega ni guarda una cámara.</p>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="flex flex-col gap-1 text-sm font-medium">Servidor para probar ONVIF
            <Select aria-label="Servidor para probar ONVIF" value={probeServerId} onChange={(event) => { cancelProbe(); setProbeServerId(event.target.value); setProbeUsername(""); }}>
              <option value="">Seleccione un servidor</option>
              {servers.data?.map((server) => <option key={server.id} value={server.id}>{server.name}</option>)}
            </Select>
          </label>
          <label className="flex flex-col gap-1 text-sm font-medium">Endpoint ONVIF (HTTP(S), IP)
            <TextInput aria-label="Endpoint ONVIF" value={probeEndpoint} onChange={(event) => { cancelProbe(); setProbeEndpoint(event.target.value); }} placeholder="https://192.0.2.10:8443/onvif/device_service" />
          </label>
          {canProbeServer && <>
            <label className="flex flex-col gap-1 text-sm font-medium">Usuario ONVIF
              <TextInput aria-label="Usuario ONVIF" autoComplete="off" value={probeUsername} onChange={(event) => setProbeUsername(event.target.value)} />
            </label>
            <label className="flex flex-col gap-1 text-sm font-medium">Contraseña ONVIF
              <TextInput aria-label="Contraseña ONVIF" type="password" autoComplete="new-password" value={probePassword} onChange={(event) => setProbePassword(event.target.value)} />
            </label>
          </>}
        </div>
        {selectedProbeServer && !hasServerPermission(me.data, "servers.manage", selectedProbeServer) && <p className="text-sm text-muted">No tiene permiso servers.manage para este servidor.</p>}
        {selectedProbeServer && hasServerPermission(me.data, "servers.manage", selectedProbeServer) && !hasServerPermission(me.data, "servers.config.secrets", selectedProbeServer) && <p className="text-sm text-muted">No tiene permiso servers.config.secrets para probar con credenciales.</p>}
        {canProbeServer && !credentialPairValid && <p className="text-sm text-muted">Ingrese usuario y contraseña juntos, o deje ambos vacíos.</p>}
        <div><button type="button" className="h-11 rounded-m3-full bg-primary px-5 text-sm font-medium text-on-primary disabled:cursor-not-allowed disabled:opacity-50" disabled={!canProbe} onClick={() => void probeOnvif()}>{probe.pending ? "Probando…" : "Probar y detectar"}</button></div>
        {probe.error && <p role="alert" className="rounded-m3-lg bg-bad/12 px-4 py-3 text-sm text-bad">{probe.error}</p>}
        {probe.pending && <p role="status" className="text-sm text-muted">Consultando el dispositivo de forma segura…</p>}
        {probe.result && <div role="status" className="rounded-m3-lg bg-surface-2 p-3 text-sm">
          <p className="font-medium">{probe.result.device_information.manufacturer} · {probe.result.device_information.model}</p>
          <p>Firmware: {probe.result.device_information.firmware_version || "No informado"}</p>
          <p>Servicios detectados: {probe.result.services.length}</p>
          <p>Hora del dispositivo: {probe.result.system_time.utc.date.year}-{String(probe.result.system_time.utc.date.month).padStart(2, "0")}-{String(probe.result.system_time.utc.date.day).padStart(2, "0")} {String(probe.result.system_time.utc.time.hour).padStart(2, "0")}:{String(probe.result.system_time.utc.time.minute).padStart(2, "0")} UTC</p>
          <p className="mt-1 text-muted">Prueba correcta; la cámara todavía no se guardó.</p>
        </div>}
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
      {filter.server_id && can(me.data, "servers.manage") && <DeletedCameras serverId={filter.server_id} />}
      <ErrorNote error={cameras.error} />
      {cameras.data && <Summary>{summarize(cameras.data)}</Summary>}
      {cameras.data?.length === 0 && <Empty>No hay cámaras que coincidan.</Empty>}
      {canBulk && picked.length > 0 && (
        <div role="region" aria-label="Selección de cámaras" className="flex flex-wrap items-center gap-3 rounded-m3-lg bg-surface-2 px-4 py-2 text-sm">
          <Button variant="filled" onClick={() => setBulkOpen(true)}>Configurar cámaras ({picked.length})</Button>
          <Button variant="text" onClick={() => setPicked([])}>Limpiar selección</Button>
          {picked.length >= BULK_CAMERA_CAP && <span role="status" className="text-xs text-warn">Máximo {BULK_CAMERA_CAP} cámaras por cambio masivo.</span>}
        </div>
      )}
      {bulkOpen && <BulkConfigureDialog target={{ cameraIds: picked }} targetLabel={`${picked.length} cámaras`} onClose={() => setBulkOpen(false)} onDone={() => setPicked([])} />}
      {!!cameras.data?.length && (
        <Table label="Cámaras">
          <thead>
            <tr>
              {canBulk && <Th><span className="sr-only">Seleccionar</span></Th>}
              <Th>Cámara</Th>
              <Th>Sitio</Th>
              <Th>Servidor</Th>
              <Th>Estado</Th>
              <Th className="text-right">FPS</Th>
              <Th>Zonas</Th>
              <Th>Clasificación</Th>
              <Th className="text-right">Configuración Frigate</Th>
            </tr>
          </thead>
          <tbody>
            {cameras.data.map((c) => (
              <tr key={c.id} className="border-t border-outline-variant align-top">
                {canBulk && (
                  <td>
                    <input type="checkbox" aria-label={`Seleccionar ${c.display_name}`} checked={picked.includes(c.id)} onChange={() => togglePicked(c.id)} />
                  </td>
                )}
                <td>
                  <div className="flex items-center gap-2 font-medium">
                    {c.display_name}
                    {c.lpr && <span className="rounded-m3-sm bg-secondary-container px-1.5 py-0.5 font-mono text-[10px] text-on-secondary-container">LPR</span>}
                    {c.onvif_profile ? (
                      <span
                        className="rounded-m3-sm bg-primary/12 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-primary"
                        title={[
                          c.onvif_profile,
                          c.onvif_version ? `v${c.onvif_version}` : null,
                          c.onvif_capabilities?.has_analytics ? "Detecciones/Analítica" : null,
                          c.onvif_capabilities?.has_ptz ? "PTZ" : null,
                        ].filter(Boolean).join(" · ")}
                      >
                        {c.onvif_profile}
                        {c.onvif_version && <span className="ml-1 opacity-75 font-normal">v{c.onvif_version}</span>}
                      </span>
                    ) : (
                      <span
                        className="rounded-m3-sm border border-outline-variant/60 px-1.5 py-0.5 font-mono text-[10px] text-muted"
                        title="Sin vincular a ONVIF"
                      >
                        Sin ONVIF
                      </span>
                    )}
                  </div>
                  {c.display_name !== c.remote_name && <div className="font-mono text-xs text-muted">{c.remote_name}</div>}
                  {c.missing_since && <div role="status" className="text-xs text-warn">{recorderIds.has(c.server_id) ? "Ya no aparece en el NVR" : "Ya no aparece en Frigate"}</div>}
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
                  {!recorderIds.has(c.server_id) && (
                    <BodyClassifySwitch
                      scope="camera"
                      id={c.id}
                      enabled={cameraOn.get(c.id)?.enabled ?? true}
                      disabled={!can(me.data, "cameras.manage")}
                      label={cameraOn.get(c.id)?.effective === false && (cameraOn.get(c.id)?.enabled ?? true) ? "Apagada en el servidor" : "Clasificación fina"}
                    />
                  )}
                </td>
                <td className="text-right">
                  {recorderIds.has(c.server_id) ? (
                    can(me.data, "servers.manage") && (
                      <Button variant="tonal" size="sm" aria-label={`Eliminar ${c.display_name}`} onClick={() => setDeletingCamera({ id: c.id, name: c.display_name })}>
                        Eliminar
                      </Button>
                    )
                  ) : (
                  <LinkButton
                    variant="tonal"
                    to="/cameras/$cameraId/frigate"
                    params={{ cameraId: c.id }}
                    aria-label={`Configuración de ${c.display_name}`}
                    title="Configuración Frigate"
                    className="inline-flex items-center gap-1.5 whitespace-nowrap rounded-m3-full px-3 py-1.5 text-xs font-semibold"
                  >
                    <Icon icon={Settings} size="xs" />
                    <span>Configurar Frigate</span>
                  </LinkButton>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
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
