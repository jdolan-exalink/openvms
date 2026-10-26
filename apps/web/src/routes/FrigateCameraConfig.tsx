import {
  Activity,
  ArrowLeft,
  Camera,
  Car,
  CircleCheck,
  Clock,
  Compass,
  Eye,
  Globe,
  HardDrive,
  History,
  Layers,
  Radio,
  RefreshCw,
  Settings,
  ShieldCheck,
  SlidersHorizontal,
  Sparkles,
  TriangleAlert,
  Video,
} from "lucide-react";
import { CameraEnabledSwitch, CameraTypeSelect } from "@/components/camera/CameraTopControls";
import { CameraWebView } from "@/components/camera/CameraWebView";
import { LprTestCard } from "@/components/provision/LprTestCard";
import { FrigateGeneralFields, GENERAL_SECTIONS, knowsSection, sectionEnabled, type GeneralFrigate } from "@/components/camera/FrigateGeneralFields";
import { FisheyeSection } from "@/components/fisheye/FisheyeSection";
import { AdvertisedAnalytics } from "@/components/provision/AdvertisedAnalytics";
import { isProvisionCamera, ProvisionLprSection } from "@/components/provision/ProvisionLprSection";
import { Icon } from "@/components/Icon";
import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { type FormEvent, useMemo, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { cameraFrigateDocQuery, cameraQuery, cameraZonesQuery, camerasQuery, classifyPolicyQuery, frigateSchemaQuery, meQuery } from "@/api/queries";
import { BodyClassifySwitch } from "@/components/BodyClassifySwitch";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { FrigateHistory } from "@/components/frigate/FrigateHistory";
import { LabelPicker } from "@/components/frigate/LabelPicker";
import { type FormCtx } from "@/components/frigate/SchemaForm";
import { SectionPanel } from "@/components/frigate/SectionPanel";
import { Modal } from "@/components/Modal";
import { ZoneEditorModal, type ZoneEditorValue } from "@/components/zones/ZoneEditorModal";
import { Button, ErrorNote, Field, LinkButton, PageHeader, Select, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import { cameraTypeOf, TYPE_TAGS } from "@/lib/cameraType";
import {
  buildPatch, deepEqual, diffValues, formatValue, isLiveSection, type JSchema, kindOf, orderSections, pathLabel, resolve, schemaAt,
  sectionLabel, setIn, validateTree,
} from "@/lib/frigateSchema";
import { pickerLabels } from "@/lib/labelEmoji";
import { loadRestart, saveRestart } from "@/lib/frigateRestart";
import { can } from "@/lib/perm";
import { persistedToEditorValue, planManagedZoneChanges, splitFrigateZones, zoneSyncStates, type ZoneChangePlan } from "@/lib/cameraZones";

type SectionResult = Schemas["FrigateSectionResult"];
const HISTORY = "__history";
const GENERAL = "__general";
const FISHEYE = "__fisheye";
const PROVISION = "__provision";
const CAMERA_WEB = "__camera_web";

function getSectionIcon(section: string) {
  switch (section) {
    case GENERAL:
      return SlidersHorizontal;
    case PROVISION:
      return Car;
    case CAMERA_WEB:
      return Globe;
    case "detect":
    case "ffmpeg":
      return Video;
    case "live":
      return Eye;
    case "objects":
    case "motion":
      return Activity;
    case "zones":
      return Layers;
    case "record":
      return HardDrive;
    case "snapshots":
      return Camera;
    case "onvif":
      return Radio;
    case HISTORY:
      return History;
    default:
      return Settings;
  }
}

const isRec = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const isEmptyMask = (v: unknown) => v == null || v === "" || (Array.isArray(v) && v.length === 0) || (isRec(v) && Object.keys(v).length === 0);

function withMask(obj: Record<string, unknown> | undefined, server: unknown, next: unknown): Record<string, unknown> | undefined {
  const current = obj?.mask;
  if (isEmptyMask(next)) {
    if (isEmptyMask(current) && (current !== undefined || server === undefined)) return obj;
    const out = { ...obj };
    if (server === undefined || server === null) delete out.mask;
    else out.mask = isRec(server) ? {} : [];
    return out;
  }
  return deepEqual(current, next) ? obj : { ...obj, mask: next };
}

export function FrigateCameraConfig() {
  const t = useT();
  const { cameraId } = useParams({ strict: false }) as { cameraId: string };
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const doc = useQuery(cameraFrigateDocQuery(cameraId));
  const camera = useQuery(cameraQuery(cameraId));
  const serverId = doc.data?.server_id;
  const schema = useQuery(frigateSchemaQuery(serverId));
  const serverCameras = useQuery({ ...camerasQuery({ server_id: serverId }), enabled: !!serverId && !!doc.data?.editable });
  const [active, setActive] = useState<string>();
  const [edits, setEdits] = useState<Record<string, unknown>>({});
  const [reviewing, setReviewing] = useState(false);
  const [bulkReviewing, setBulkReviewing] = useState(false);
  const [bulkTargets, setBulkTargets] = useState<string[]>([]);
  const [bulkResults, setBulkResults] = useState<Array<{ id: string; name: string; error?: string }>>([]);
  const [results, setResults] = useState<SectionResult[]>([]);
  const [zoneEditor, setZoneEditor] = useState(false);
  const canManageCameras = can(me.data, "cameras.manage");
  // Persisted zones are the source of truth for OpenVMS-owned zones; without the permission (or the
  // list) the editor falls back to editing the Frigate config directly.
  const persistedZones = useQuery({ ...cameraZonesQuery(cameraId), enabled: canManageCameras });
  const managedZones = canManageCameras ? persistedZones.data : undefined;
  const [restartPending, setRestartPending] = useState<Record<string, boolean>>({});

  const root = schema.data as JSchema | undefined;
  const config = useMemo(() => (doc.data?.config ?? {}) as Record<string, unknown>, [doc.data]);
  const camProps = useMemo(() => (root ? (resolve(root, root).node.properties ?? {}) : {}), [root]);
  const sections = useMemo(
    () => orderSections([...Object.keys(camProps), ...Object.keys(config)]).filter((s) => s !== "birdseye"),
    [camProps, config]
  );
  const version = doc.data?.frigate_version ?? "";
  const readOnly = !doc.data?.editable;
  // The camera web view opens a session that needs the same permissions as the ONVIF probe.
  const canOpenCameraWeb = can(me.data, "servers.manage") && can(me.data, "servers.config.secrets");
  const canRestart = can(me.data, "servers.restart");
  const secretsVisible = !!doc.data?.secrets_visible;
  const needsRestart = serverId ? (restartPending[serverId] ?? loadRestart(serverId)) : false;
  const setNeedsRestart = (on: boolean) => {
    if (!serverId) return;
    saveRestart(serverId, on);
    setRestartPending((p) => ({ ...p, [serverId]: on }));
  };

  const valueOf = (s: string) => (s in edits ? edits[s] : config[s]);
  const dirty = sections.filter((s) => s in edits && !deepEqual(edits[s], config[s]));
  const errors = useMemo(() => {
    if (!root) return {} as Record<string, string>;
    const out: Record<string, string> = {};
    for (const s of dirty) validateTree(root, camProps[s] ?? {}, edits[s], [s], out);
    return out;
  }, [root, camProps, edits, dirty]);
  const errorCount = Object.keys(errors).length;

  const buildSections = () => {
    const out: Record<string, unknown> = {};
    for (const s of dirty) {
      const isMap = (path: string[]) => !!root && kindOf(schemaAt(root, camProps[s] ?? {}, path) ?? {}, root) === "map";
      out[s] = buildPatch(config[s], edits[s], isMap);
    }
    return out;
  };

  const apply = useMutation({
    mutationFn: async () =>
      unwrap(await api.PATCH("/api/v1/cameras/{cameraId}/frigate-config", { params: { path: { cameraId } }, body: { sections: buildSections() } })),
    onSuccess: async (res) => {
      setResults(res.sections);
      if (res.restart_required) setNeedsRestart(true);
      setEdits({});
      setReviewing(false);
      await qc.invalidateQueries({ queryKey: ["cameras", cameraId] });
      await qc.invalidateQueries({ queryKey: ["servers", serverId, "frigate-revisions"] });
    },
    onError: async () => {
      await qc.invalidateQueries({ queryKey: ["cameras", cameraId] });
    },
  });

  const bulkApply = useMutation({
    mutationFn: async (targets: Array<{ id: string; name: string }>) => {
      const patch = { sections: buildSections() };
      const outcomes: Array<{ id: string; name: string; error?: string; result?: { restart_required: boolean; sections: SectionResult[] } }> = [];
      for (const target of targets) {
        try {
          const result = unwrap(await api.PATCH("/api/v1/cameras/{cameraId}/frigate-config", { params: { path: { cameraId: target.id } }, body: patch }));
          outcomes.push({ ...target, result });
        } catch (error) {
          outcomes.push({ ...target, error: error instanceof Error ? error.message : "No se pudo aplicar." });
        }
      }
      return outcomes;
    },
    onSuccess: async (outcomes) => {
      setBulkResults(outcomes);
      const current = outcomes.find((item) => item.id === cameraId);
      if (current?.result) setResults(current.result.sections);
      if (outcomes.some((item) => item.result?.restart_required)) setNeedsRestart(true);
      for (const target of outcomes) {
        await qc.invalidateQueries({ queryKey: ["cameras", target.id] });
      }
      await qc.invalidateQueries({ queryKey: ["servers", serverId, "frigate-revisions"] });
      if (outcomes.every((item) => !item.error)) {
        setEdits({});
        setBulkReviewing(false);
      }
    },
  });

  const otherServerCameras = (serverCameras.data ?? []).filter((item) => item.id !== cameraId);
  const bulkTargetRows = [
    { id: cameraId, display_name: doc.data?.camera_name ?? cameraId },
    ...otherServerCameras.map(({ id, display_name }) => ({ id, display_name })),
  ];

  const activeSection = active ?? GENERAL;

  const frigateZones = () => (isRec(valueOf("zones")) ? valueOf("zones") : {}) as ZoneEditorValue["zones"];

  const zoneEditorValue = (): ZoneEditorValue => {
    const objects = (valueOf("objects") ?? {}) as Record<string, unknown>;
    const filters = (isRec(objects.filters) ? objects.filters : {}) as Record<string, Record<string, unknown>>;
    const filterMasks: Record<string, unknown> = {};
    for (const [label, f] of Object.entries(filters)) if (isRec(f) && f.mask !== undefined) filterMasks[label] = f.mask;
    const common = {
      motionMask: (valueOf("motion") as Record<string, unknown> | undefined)?.mask,
      objectMask: objects.mask,
      objectFilterMasks: filterMasks,
    };
    if (!managedZones) return { zones: frigateZones(), ...common };
    const { foreign } = splitFrigateZones(frigateZones(), new Set(managedZones.map((z) => z.name)));
    const persisted = persistedToEditorValue(managedZones);
    return { zones: { ...foreign, ...persisted.zones }, managedZones: persisted.managedZones, ...common };
  };

  const persistZones = useMutation({
    mutationFn: async (plan: ZoneChangePlan) => {
      // Removals first so a rename into a freed name cannot collide.
      for (const zoneId of plan.remove) unwrap(await api.DELETE("/api/v1/cameras/{cameraId}/zones/{zoneId}", { params: { path: { cameraId, zoneId } } }));
      for (const { id: zoneId, input } of plan.update) unwrap(await api.PUT("/api/v1/cameras/{cameraId}/zones/{zoneId}", { params: { path: { cameraId, zoneId } }, body: input }));
      for (const input of plan.create) unwrap(await api.POST("/api/v1/cameras/{cameraId}/zones", { params: { path: { cameraId } }, body: input }));
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["cameras", cameraId, "zones"] }),
  });

  const retryZoneSync = useMutation({
    mutationFn: async (zoneId: string) => unwrap(await api.POST("/api/v1/cameras/{cameraId}/zones/{zoneId}/sync", { params: { path: { cameraId, zoneId } } })),
    onSettled: () => qc.invalidateQueries({ queryKey: ["cameras", cameraId, "zones"] }),
  });

  const closeZoneEditor = () => {
    setZoneEditor(false);
    persistZones.reset();
  };

  const submitZoneEditor = (next: ZoneEditorValue) => {
    if (!managedZones || !next.managedZones) return saveZoneEditor(next);
    const plan = planManagedZoneChanges(managedZones, next);
    // Foreign zones keep the Frigate-config path; OpenVMS-owned keys reach the patch unchanged.
    const { preserved } = splitFrigateZones(frigateZones(), new Set(managedZones.map((z) => z.name)));
    const foreignZones = Object.fromEntries(Object.entries(next.zones).filter(([name]) => !(name in next.managedZones!)));
    persistZones.mutate(plan, { onSuccess: () => saveZoneEditor({ ...next, zones: { ...foreignZones, ...preserved } }) });
  };

  const saveZoneEditor = (next: ZoneEditorValue) => {
    const serverObjects = config.objects as Record<string, unknown> | undefined;
    const serverFilters = (isRec(serverObjects?.filters) ? serverObjects.filters : {}) as Record<string, Record<string, unknown>>;
    const draft: Record<string, unknown> = {};
    draft.zones = Object.keys(next.zones).length || config.zones !== undefined ? next.zones : undefined;
    draft.motion = withMask(valueOf("motion") as Record<string, unknown> | undefined, (config.motion as Record<string, unknown> | undefined)?.mask, next.motionMask);
    let objects = withMask(valueOf("objects") as Record<string, unknown> | undefined, serverObjects?.mask, next.objectMask);
    const filters = { ...(isRec(objects?.filters) ? objects.filters : {}) } as Record<string, Record<string, unknown>>;
    let filtersChanged = false;
    for (const label of new Set([...Object.keys(filters), ...Object.keys(next.objectFilterMasks)])) {
      const before = filters[label];
      const after = withMask(before, serverFilters[label]?.mask, next.objectFilterMasks[label]);
      if (after === before) continue;
      filtersChanged = true;
      if (after === undefined || (Object.keys(after).length === 0 && serverFilters[label] === undefined)) delete filters[label];
      else filters[label] = after;
    }
    if (filtersChanged) objects = { ...objects, filters };
    draft.objects = objects;
    setEdits((e) => {
      const out = { ...e };
      for (const [s, v] of Object.entries(draft)) {
        if (v === valueOf(s) && !(s === "zones" && v === undefined)) continue;
        if (deepEqual(v, config[s])) delete out[s];
        else out[s] = v;
      }
      return out;
    });
    closeZoneEditor();
  };

  const detectDraft = valueOf("detect") as Record<string, unknown> | undefined;
  const objectsDraft = valueOf("objects") as Record<string, unknown> | undefined;
  const zoneLabels = pickerLabels(Object.keys(isRec(objectsDraft?.filters) ? objectsDraft.filters : {}), Array.isArray(objectsDraft?.track) ? (objectsDraft.track as string[]) : []);
  const ctx: FormCtx = { root: root ?? {}, secretsVisible, readOnly };
  const setSection = (section: string, value: unknown) => setEdits((e) => ({ ...e, [section]: value }));

  const header = (
    <PageHeader
      title={`Frigate · ${doc.data?.camera_name ?? t("common.camera")}`}
      description={version ? t("settings.cameraConfig", { version }) : undefined}
      actions={
        <LinkButton to="/cameras">
          <Icon icon={ArrowLeft} size="xs" /> Volver a cámaras
        </LinkButton>
      }
    />
  );

  if (doc.isLoading || (serverId && schema.isLoading)) {
    return <div className="mx-auto max-w-6xl">{header}<p className="mt-6 text-sm text-muted">Cargando configuración de Frigate…</p></div>;
  }
  if (doc.error || schema.error || !doc.data || !root) {
    return <div className="mx-auto flex max-w-6xl flex-col gap-4">{header}<ErrorNote error={doc.error ?? schema.error ?? new Error("Configuración de Frigate no disponible.")} /></div>;
  }

  // Filter sections that are handled in our unified sections:
  const unifiedHandled = new Set(["detect", "objects", "motion", "record", "snapshots", "zones", "review", "birdseye", ...GENERAL_SECTIONS]);
  const otherSections = sections.filter((s) => !unifiedHandled.has(s));

  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-5">
      {needsRestart && (
        <RestartBanner serverId={doc.data.server_id} canRestart={canRestart} onDone={() => setNeedsRestart(false)} />
      )}
      {readOnly && (
        <p role="status" className="rounded-m3-lg bg-warn/10 px-4 py-3 text-sm text-warn">
          Necesitas el permiso servers.config para editar esta configuración. Solo lectura.
        </p>
      )}
      {!secretsVisible && !readOnly && (
        <p className="text-xs text-muted">Las credenciales (rutas de streams, usuario y contraseña ONVIF) están ocultas: requieren permiso de credenciales.</p>
      )}
      {results.length > 0 && <ResultList results={results} />}
      <ErrorNote
        error={apply.error ? new Error(`${apply.error.message} Las secciones anteriores pudieron aplicarse: revisa el historial o recarga para ver el estado actual.`) : null}
      />

      {/* Avigilon HUD Viewfinder & Specs Header */}
      <CameraIdentity name={doc.data.camera_name} version={version} config={config} camera={camera.data} cameraId={cameraId} />

      {/* Nx Witness / Avigilon Control Room Layout: Left Sidebar + Main Settings */}
      <div className="grid grid-cols-1 md:grid-cols-[260px_1fr] lg:grid-cols-[280px_1fr] gap-6 items-start">
        {/* Left Sidebar Navigation */}
        <aside className="flex flex-col gap-3 rounded-m3-2xl border border-outline-variant/60 bg-surface-1 p-3 shadow-sm md:sticky md:top-4">
          <div className="px-3 py-2 flex items-center justify-between border-b border-outline-variant/40">
            <span className="font-mono text-xs font-bold text-muted uppercase tracking-wider">Opciones de Cámara</span>
            {dirty.length > 0 && (
              <span className="rounded-m3-xs bg-primary/20 px-2 py-0.5 text-[10px] font-bold text-primary animate-pulse">
                {dirty.length} {dirty.length === 1 ? "cambio" : "cambios"}
              </span>
            )}
          </div>

          <nav aria-label="Secciones de configuración" className="flex flex-col gap-1.5">
            {/* 1. General */}
            <div className="flex flex-col gap-1 rounded-m3-xl border border-outline-variant/40 bg-surface-2 p-1.5">
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  aria-current={activeSection === GENERAL ? "page" : undefined}
                  onClick={() => setActive(GENERAL)}
                  className={cn(
                    "flex min-h-10 flex-1 items-center gap-2 rounded-m3-lg px-3 text-xs font-bold transition-colors text-left",
                    activeSection === GENERAL
                      ? "bg-primary text-on-primary shadow-sm"
                      : "text-on-surface hover:bg-surface-3"
                  )}
                >
                  <Icon icon={SlidersHorizontal} size="xs" />
                  <span>General</span>
                  {GENERAL_SECTIONS.some((g) => dirty.includes(g)) && <span aria-label="con cambios" className="ml-auto text-primary">●</span>}
                </button>
              </div>
            </div>

            {/* 2. Detección y Analítica */}
            <div className="flex flex-col gap-1 rounded-m3-xl border border-outline-variant/40 bg-surface-2 p-1.5">
              <button
                type="button"
                aria-current={activeSection === "detect" ? "page" : undefined}
                onClick={() => setActive("detect")}
                className={cn(
                  "flex min-h-10 w-full items-center justify-between rounded-m3-lg px-3 text-xs font-bold transition-colors text-left",
                  activeSection === "detect"
                    ? "bg-primary text-on-primary shadow-sm"
                    : "text-on-surface hover:bg-surface-3"
                )}
              >
                <span className="flex items-center gap-2">
                  <Icon icon={Activity} size="xs" />
                  <span>Detección</span>
                </span>
                <span className="flex items-center gap-1.5">
                  {dirty.includes("detect") && <span aria-label="con cambios" className="text-primary animate-pulse">●</span>}
                  <LiveBadge live={isLiveSection("detect")} />
                </span>
              </button>
              <div className="grid grid-cols-2 gap-1 pt-0.5">
                <button
                  type="button"
                  aria-current={activeSection === "objects" ? "page" : undefined}
                  onClick={() => setActive("objects")}
                  className={cn(
                    "flex items-center justify-between rounded-m3-md px-2.5 py-1.5 text-xs font-bold transition-colors",
                    activeSection === "objects"
                      ? "bg-primary text-on-primary"
                      : "text-on-surface-variant hover:bg-surface-3"
                  )}
                >
                  <span className="flex items-center gap-1.5">
                    <Icon icon={Sparkles} size="xs" />
                    <span>Objetos</span>
                  </span>
                  {dirty.includes("objects") && <span className="text-primary">●</span>}
                </button>
                <button
                  type="button"
                  aria-current={activeSection === "motion" ? "page" : undefined}
                  onClick={() => setActive("motion")}
                  className={cn(
                    "flex items-center justify-between rounded-m3-md px-2.5 py-1.5 text-xs font-bold transition-colors",
                    activeSection === "motion"
                      ? "bg-primary text-on-primary"
                      : "text-on-surface-variant hover:bg-surface-3"
                  )}
                >
                  <span className="flex items-center gap-1.5">
                    <Icon icon={Video} size="xs" />
                    <span>Movimiento</span>
                  </span>
                  {dirty.includes("motion") && <span className="text-primary">●</span>}
                </button>
              </div>
            </div>

            {/* 3. Grabación y Capturas */}
            <div className="flex flex-col gap-1 rounded-m3-xl border border-outline-variant/40 bg-surface-2 p-1.5">
              <button
                type="button"
                aria-current={activeSection === "record" ? "page" : undefined}
                onClick={() => setActive("record")}
                className={cn(
                  "flex min-h-10 w-full items-center justify-between rounded-m3-lg px-3 text-xs font-bold transition-colors text-left",
                  activeSection === "record"
                    ? "bg-primary text-on-primary shadow-sm"
                    : "text-on-surface hover:bg-surface-3"
                )}
              >
                <span className="flex items-center gap-2">
                  <Icon icon={HardDrive} size="xs" />
                  <span>Grabación</span>
                </span>
                <span className="flex items-center gap-1.5">
                  {dirty.includes("record") && <span aria-label="con cambios" className="text-primary animate-pulse">●</span>}
                  <LiveBadge live={isLiveSection("record")} />
                </span>
              </button>
              <div className="pt-0.5">
                <button
                  type="button"
                  aria-current={activeSection === "snapshots" ? "page" : undefined}
                  onClick={() => setActive("snapshots")}
                  className={cn(
                    "flex w-full items-center justify-between rounded-m3-md px-2.5 py-1.5 text-xs font-bold transition-colors",
                    activeSection === "snapshots"
                      ? "bg-primary text-on-primary"
                      : "text-on-surface-variant hover:bg-surface-3"
                  )}
                >
                  <span className="flex items-center gap-1.5">
                    <Icon icon={Camera} size="xs" />
                    <span>Capturas</span>
                  </span>
                  {dirty.includes("snapshots") && <span className="text-primary">●</span>}
                </button>
              </div>
            </div>

            {/* 4. Zonas y Revisión */}
            <div className="flex flex-col gap-1 rounded-m3-xl border border-outline-variant/40 bg-surface-2 p-1.5">
              <button
                type="button"
                aria-current={activeSection === "zones" ? "page" : undefined}
                onClick={() => setActive("zones")}
                className={cn(
                  "flex min-h-10 w-full items-center justify-between rounded-m3-lg px-3 text-xs font-bold transition-colors text-left",
                  activeSection === "zones"
                    ? "bg-primary text-on-primary shadow-sm"
                    : "text-on-surface hover:bg-surface-3"
                )}
              >
                <span className="flex items-center gap-2">
                  <Icon icon={Layers} size="xs" />
                  <span>Zonas</span>
                </span>
                <span className="flex items-center gap-1.5">
                  {dirty.includes("zones") && <span aria-label="con cambios" className="text-primary animate-pulse">●</span>}
                  <LiveBadge live={isLiveSection("zones")} />
                </span>
              </button>
              <div className="pt-0.5">
                <button
                  type="button"
                  aria-current={activeSection === "review" ? "page" : undefined}
                  onClick={() => setActive("review")}
                  className={cn(
                    "flex w-full items-center justify-between rounded-m3-md px-2.5 py-1.5 text-xs font-bold transition-colors",
                    activeSection === "review"
                      ? "bg-primary text-on-primary"
                      : "text-on-surface-variant hover:bg-surface-3"
                  )}
                >
                  <span className="flex items-center gap-1.5">
                    <Icon icon={Eye} size="xs" />
                    <span>Revisión y Alertas</span>
                  </span>
                  {dirty.includes("review") && <span className="text-primary">●</span>}
                </button>
              </div>
            </div>

            {/* 5. Modo Fisheye / 360° */}
            <div className="flex flex-col gap-1 rounded-m3-xl border border-outline-variant/40 bg-surface-2 p-1.5">
              <button
                type="button"
                aria-current={activeSection === FISHEYE ? "page" : undefined}
                onClick={() => setActive(FISHEYE)}
                className={cn(
                  "flex min-h-10 w-full items-center justify-between rounded-m3-lg px-3 text-xs font-bold transition-colors text-left",
                  activeSection === FISHEYE
                    ? "bg-primary text-on-primary shadow-sm"
                    : "text-on-surface hover:bg-surface-3"
                )}
              >
                <span className="flex items-center gap-2">
                  <Icon icon={Compass} size="xs" />
                  <span>Fisheye / 360°</span>
                </span>
                {(camera.data?.tags?.includes("fisheye") || camera.data?.tags?.includes("ojo_de_pez")) && (
                  <span className="rounded-m3-xs bg-cyan-400/20 text-cyan-300 px-1 py-0.5 text-[9px] font-mono">
                    Activo
                  </span>
                )}
              </button>
            </div>

            {/* 6. Modo Provision LPR */}
            {(isProvisionCamera(camera.data) || camera.data?.tags?.includes("provision") || activeSection === PROVISION) && (
              <div className="flex flex-col gap-1 rounded-m3-xl border border-outline-variant/40 bg-surface-2 p-1.5">
                <button
                  type="button"
                  aria-current={activeSection === PROVISION ? "page" : undefined}
                  onClick={() => setActive(PROVISION)}
                  className={cn(
                    "flex min-h-10 w-full items-center justify-between rounded-m3-lg px-3 text-xs font-bold transition-colors text-left",
                    activeSection === PROVISION
                      ? "bg-primary text-on-primary shadow-sm"
                      : "text-on-surface hover:bg-surface-3"
                  )}
                >
                  <span className="flex items-center gap-2">
                    <Icon icon={Car} size="xs" />
                    <span>Provision LPR</span>
                  </span>
                  <span className="flex items-center gap-1">
                    {camera.data?.tags?.includes("provision") &&
                      !camera.data?.onvif_capabilities?.manufacturer?.toLowerCase().includes("provision") && (
                        <span className="rounded-m3-xs bg-primary/20 text-primary px-1.5 py-0.5 text-[9px] font-mono font-bold">
                          Forzada
                        </span>
                      )}
                    {camera.data?.onvif_capabilities?.manufacturer?.toLowerCase().includes("provision") && (
                      <span className="rounded-m3-xs bg-ok/20 text-ok px-1.5 py-0.5 text-[9px] font-mono font-bold">
                        ONVIF
                      </span>
                    )}
                  </span>
                </button>
              </div>
            )}

            {/* Web propia de la cámara, a través del agente */}
            {canOpenCameraWeb && (
              <div className="flex flex-col gap-1 rounded-m3-xl border border-outline-variant/40 bg-surface-2 p-1.5">
                <button
                  type="button"
                  aria-current={activeSection === CAMERA_WEB ? "page" : undefined}
                  onClick={() => setActive(CAMERA_WEB)}
                  className={cn(
                    "flex min-h-10 w-full items-center justify-between rounded-m3-lg px-3 text-xs font-bold transition-colors text-left",
                    activeSection === CAMERA_WEB
                      ? "bg-primary text-on-primary shadow-sm"
                      : "text-on-surface hover:bg-surface-3"
                  )}
                >
                  <span className="flex items-center gap-2">
                    <Icon icon={Globe} size="xs" />
                    <span>Web de la cámara</span>
                  </span>
                </button>
              </div>
            )}

            {/* 6. Secciones adicionales avanzadas */}
            {otherSections.map((s) => {
              const live = isLiveSection(s);
              const SIcon = getSectionIcon(s);
              return (
                <button
                  key={s}
                  type="button"
                  aria-current={activeSection === s ? "page" : undefined}
                  onClick={() => setActive(s)}
                  className={cn(
                    "flex min-h-10 w-full items-center justify-between rounded-m3-lg px-3 text-xs font-bold transition-colors text-left",
                    activeSection === s
                      ? "bg-primary text-on-primary shadow-sm"
                      : "text-on-surface hover:bg-surface-2"
                  )}
                >
                  <span className="flex items-center gap-2">
                    <Icon icon={SIcon} size="xs" />
                    <span>{sectionLabel(s)}</span>
                  </span>
                  <span className="flex items-center gap-1.5">
                    {dirty.includes(s) && <span aria-label="con cambios" className="text-primary">●</span>}
                    <LiveBadge live={live} />
                  </span>
                </button>
              );
            })}

            {/* 7. Historial */}
            <button
              type="button"
              aria-current={activeSection === HISTORY ? "page" : undefined}
              onClick={() => setActive(HISTORY)}
              className={cn(
                "flex min-h-10 w-full items-center gap-2 rounded-m3-lg px-3 text-xs font-bold transition-colors text-left mt-2 border-t border-outline-variant/40 pt-3",
                activeSection === HISTORY
                  ? "bg-primary text-on-primary shadow-sm"
                  : "text-on-surface hover:bg-surface-2"
              )}
            >
              <Icon icon={History} size="xs" />
              <span>Historial</span>
            </button>
          </nav>
        </aside>

        {/* Right Main Content Section */}
        <section
          aria-label={
            activeSection === HISTORY
              ? "Historial"
              : activeSection === GENERAL
              ? t("common.general")
              : activeSection === FISHEYE
              ? "Fisheye"
              : activeSection === PROVISION
              ? "Provision LPR"
              : activeSection === CAMERA_WEB
              ? "Acceso web a la cámara"
              : sectionLabel(activeSection ?? "")
          }
          className="flex min-w-0 flex-1 flex-col gap-5 rounded-m3-2xl bg-surface-1 p-5 border border-outline-variant/60 shadow-sm"
        >
          {activeSection === GENERAL ? (
            <CameraGeneral
              cameraId={cameraId}
              canManage={can(me.data, "cameras.manage")}
              frigate={{ root, camProps, config, doc: doc.data, ctx, valueOf, setSection }}
              onApplyToFrigate={(patch) => {
                setEdits((e) => ({ ...e, onvif: patch }));
                setActive("onvif");
              }}
            />
          ) : activeSection === FISHEYE ? (
            <FisheyeSection
              cameraId={cameraId}
              cameraName={doc.data.camera_name}
              snapshotUrl={`/media/v1/cameras/${cameraId}/snapshot.jpg`}
              canManage={!readOnly}
            />
          ) : activeSection === PROVISION ? (
            camera.data && (
              <ProvisionLprSection
                camera={camera.data}
                onCameraUpdate={() => {
                  void qc.invalidateQueries({ queryKey: ["cameras", cameraId] });
                  void qc.invalidateQueries({ queryKey: ["cameras"] });
                }}
              />
            )
          ) : activeSection === CAMERA_WEB ? (
            <CameraWebView cameraId={cameraId} />
          ) : activeSection === HISTORY ? (
            <FrigateHistory
              serverId={doc.data.server_id}
              cameraId={cameraId}
              secretsVisible={secretsVisible}
              canRollback={!readOnly && secretsVisible}
              onRestored={() => setNeedsRestart(true)}
            />
          ) : (
            activeSection && (
              <SectionPanel
                section={activeSection}
                schema={camProps[activeSection]}
                value={valueOf(activeSection)}
                onChange={(v) => setEdits((e) => ({ ...e, [activeSection]: v }))}
                ctx={ctx}
                camera={doc.data}
                config={config}
                onEditZones={() => setZoneEditor(true)}
              />
            )
          )}

          {activeSection !== HISTORY &&
            activeSection !== GENERAL &&
            activeSection !== FISHEYE &&
            activeSection !== PROVISION &&
            activeSection !== CAMERA_WEB &&
            Object.entries(errors).filter(([p]) => p.split(".")[0] === activeSection).length > 0 && (
              <p role="alert" className="text-xs text-bad">
                Hay valores no válidos en esta sección.
              </p>
            )}
        </section>
      </div>

      {!readOnly && (
        <div className="sticky bottom-0 z-10 flex flex-wrap items-center justify-between gap-2 rounded-m3-xl bg-surface-3 px-5 py-3 shadow-md">
          <span role="status" className="text-sm text-muted">
            {dirty.length ? `${dirty.length} ${dirty.length === 1 ? "sección modificada" : "secciones modificadas"}` : "Sin cambios pendientes"}
            {errorCount > 0 && <span className="text-bad"> · {errorCount} {errorCount === 1 ? "error" : "errores"}</span>}
          </span>
          <div className="flex gap-2">
            <Button variant="text" disabled={!dirty.length} onClick={() => { setEdits({}); apply.reset(); }}>Descartar</Button>
            <Button
              variant="secondary"
              disabled={!dirty.length || errorCount > 0 || !serverId}
              onClick={() => { setBulkTargets([cameraId]); setBulkResults([]); bulkApply.reset(); setBulkReviewing(true); }}
            >
              Aplicar a varias cámaras
            </Button>
            <Button variant="primary" disabled={!dirty.length || errorCount > 0} onClick={() => setReviewing(true)}>Revisar cambios</Button>
          </div>
        </div>
      )}

      {zoneEditor && (
        <ZoneEditorModal
          open
          onClose={closeZoneEditor}
          cameraId={cameraId}
          snapshotUrl={`/media/v1/cameras/${cameraId}/snapshot.jpg`}
          frameWidth={typeof detectDraft?.width === "number" ? detectDraft.width : undefined}
          frameHeight={typeof detectDraft?.height === "number" ? detectDraft.height : undefined}
          value={zoneEditorValue()}
          frigateVersion={version}
          onSave={submitZoneEditor}
          syncStates={managedZones ? zoneSyncStates(managedZones) : undefined}
          onRetrySync={managedZones ? (zoneId) => retryZoneSync.mutate(zoneId) : undefined}
          saving={persistZones.isPending}
          saveError={persistZones.isError ? `No se pudieron guardar las zonas: ${persistZones.error.message}` : null}
          renderLabelPicker={({ value, onChange, ariaLabel }) => <LabelPicker label={ariaLabel} value={value} onChange={onChange} />}
          availableLabels={zoneLabels}
        />
      )}

      {reviewing && (
        <Modal title="Revisar cambios" onClose={() => setReviewing(false)}>
          <DiffList sections={dirty} before={config} after={edits} />
          <ErrorNote error={apply.error} />
          <div className="flex justify-end gap-2">
            <Button variant="text" onClick={() => setReviewing(false)}>Volver</Button>
            <Button variant="primary" disabled={apply.isPending} onClick={() => apply.mutate()}>
              {apply.isPending ? "Aplicando…" : "Aplicar"}
            </Button>
          </div>
        </Modal>
      )}

      {bulkReviewing && (
        <Modal title="Aplicar a varias cámaras" onClose={() => setBulkReviewing(false)}>
          <p className="text-sm text-muted">Se aplicarán las mismas secciones a cámaras visibles de este servidor. La cámara actual está incluida. Cada cambio respeta los permisos del servidor.</p>
          <fieldset className="flex max-h-64 flex-col gap-2 overflow-y-auto rounded-m3-lg bg-surface-2 p-3" disabled={bulkApply.isPending}>
            <legend className="px-1 text-sm font-medium">Cámaras de este servidor</legend>
            {bulkTargetRows.map((target) => (
              <label key={target.id} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={bulkTargets.includes(target.id)}
                  disabled={target.id === cameraId}
                  onChange={(event) => setBulkTargets((selected) => event.target.checked
                    ? [...selected, target.id]
                    : selected.filter((id) => id !== target.id))}
                />
                <span>{target.display_name}{target.id === cameraId ? " (actual)" : ""}</span>
              </label>
            ))}
            {serverCameras.isLoading && <p className="text-xs text-muted">Cargando cámaras…</p>}
            {serverCameras.error && <ErrorNote error={serverCameras.error} />}
          </fieldset>
          {bulkResults.length > 0 && (
            <ul aria-label="Resultado de aplicación múltiple" className="flex flex-col gap-1 text-sm">
              {bulkResults.map((item) => (
                <li key={item.id} role={item.error ? "alert" : "status"}>
                  {item.name}: {item.error ? `no aplicada — ${item.error}` : "aplicada"}
                </li>
              ))}
            </ul>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="text" onClick={() => setBulkReviewing(false)}>Cerrar</Button>
            <Button
              variant="primary"
              disabled={bulkApply.isPending || bulkTargets.length === 0 || serverCameras.isLoading || !!serverCameras.error}
              onClick={() => bulkApply.mutate(bulkTargetRows.filter((item) => bulkTargets.includes(item.id)).map(({ id, display_name }) => ({ id, name: display_name })))}
            >
              {bulkApply.isPending ? "Aplicando…" : "Aplicar cambios"}
            </Button>
          </div>
        </Modal>
      )}
    </div>
  );
}

function displayEndpointHost(endpoint: string): string {
  try {
    const parsed = new URL(endpoint);
    return parsed.hostname;
  } catch {
    return endpoint;
  }
}

function CameraIdentity({
  name,
  version,
  config,
  camera,
  cameraId,
}: {
  name: string;
  version: string;
  config: Record<string, unknown>;
  camera?: Schemas["Camera"];
  cameraId?: string;
}) {
  const [imgError, setImgError] = useState(false);
  const detect = isRec(config.detect) ? config.detect : undefined;
  const width = typeof detect?.width === "number" ? detect.width : undefined;
  const height = typeof detect?.height === "number" ? detect.height : undefined;
  const fps = typeof detect?.fps === "number" ? detect.fps : undefined;
  const onvifCaps = camera?.onvif_capabilities;
  const isOnline = camera?.enabled && camera.status === "online";
  const snapshotUrl = cameraId ? `/media/v1/cameras/${cameraId}/snapshot.jpg?h=360` : undefined;

  return (
    <div className="flex flex-col gap-4 rounded-m3-2xl border border-outline-variant/60 bg-gradient-to-b from-surface-1 to-surface-dim p-4 sm:p-5 shadow-sm">
      <div className="flex flex-col gap-5 lg:flex-row lg:items-stretch lg:gap-6">
        {/* Avigilon Live Snapshot Viewfinder */}
        {snapshotUrl && (
          <div className="relative aspect-video w-full shrink-0 overflow-hidden rounded-m3-xl border border-outline-variant/80 bg-black/80 sm:w-80 lg:w-96 shadow-inner">
            {!imgError ? (
              <img
                src={snapshotUrl}
                alt={`Vista previa de ${name}`}
                className="size-full object-cover"
                onError={() => setImgError(true)}
              />
            ) : (
              <div className="flex size-full flex-col items-center justify-center gap-2 text-muted">
                <Icon icon={Video} size="md" className="opacity-40" />
                <span className="font-mono text-xs">Sin vista previa</span>
              </div>
            )}
            {/* Avigilon HUD Overlays */}
            <div className="pointer-events-none absolute inset-0 flex flex-col justify-between p-2.5 bg-gradient-to-t from-black/70 via-transparent to-black/60">
              <div className="flex items-center justify-between gap-2">
                <span className="flex items-center gap-1.5 rounded-m3-xs bg-black/60 px-2 py-0.5 font-mono text-[10px] font-bold tracking-wider text-white backdrop-blur-md">
                  <span className={cn("size-2 rounded-full", isOnline ? "bg-ok animate-pulse" : "bg-muted")} />
                  {isOnline ? "EN VIVO" : camera?.enabled ? "ONLINE" : "OFFLINE"}
                </span>
                <span className="rounded-m3-xs bg-black/60 px-1.5 py-0.5 font-mono text-[10px] text-white/90 backdrop-blur-md">
                  {width && height ? `${width}×${height}` : "1080p"}
                </span>
              </div>
              <div className="flex items-center justify-between gap-2 text-[10px] font-mono text-white/90">
                <span className="rounded-m3-xs bg-black/60 px-1.5 py-0.5 backdrop-blur-md">
                  {fps ? `${fps} FPS` : (camera?.fps ? `${camera.fps.toFixed(1)} FPS` : "Detección activa")}
                </span>
                <span className="rounded-m3-xs bg-black/60 px-1.5 py-0.5 backdrop-blur-md">
                  {camera?.default_live_quality === "main" ? "STREAM MAIN" : "STREAM SUB"}
                </span>
              </div>
            </div>
          </div>
        )}

        {/* Avigilon Device Telemetry Header */}
        <div className="flex min-w-0 flex-1 flex-col justify-between gap-3">
          <div className="flex flex-col gap-2">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="flex flex-col gap-1">
                <div className="flex flex-wrap items-center gap-2">
                  <h1 className="text-[22px] font-bold tracking-tight text-ink sm:text-2xl">{name}</h1>
                  {camera?.lpr && (
                    <span className="rounded-m3-sm bg-secondary-container px-2 py-0.5 font-mono text-xs font-bold text-on-secondary-container">
                      LPR
                    </span>
                  )}
                </div>
                <p className="font-mono text-xs text-muted">
                  {camera?.remote_name ? `ID / Origen Frigate: ${camera.remote_name}` : `Cámara: ${name}`}
                  {version && ` · Frigate ${version}`}
                  {(onvifCaps?.manufacturer || onvifCaps?.model) && ` · ${[onvifCaps.manufacturer, onvifCaps.model].filter(Boolean).join(" ")}`}
                </p>
              </div>
              <LinkButton to="/cameras" variant="tonal" className="self-start text-xs">
                <Icon icon={ArrowLeft} size="xs" /> Volver a cámaras
              </LinkButton>
            </div>
          </div>

          {/* Telemetry grid (ONVIF facts live in the "Capacidades ONVIF" popup of General) */}
          <dl className="grid grid-cols-2 gap-2 text-xs">
            <div className="flex flex-col gap-0.5 rounded-m3-lg border border-outline-variant/40 bg-surface-2 p-3">
              <dt className="text-muted font-medium">Versión</dt>
              <dd className="font-mono text-ink font-semibold">{version || "—"}</dd>
              <span className="truncate text-[10px] text-muted">Frigate NVR</span>
            </div>

            <div className="flex flex-col gap-0.5 rounded-m3-lg border border-outline-variant/40 bg-surface-2 p-3">
              <dt className="text-muted font-medium">Resolución</dt>
              <dd className="font-mono text-ink font-semibold">{width && height ? `${width}×${height}` : "—"}</dd>
              <span className="font-mono text-[10px] text-muted">
                {fps ? `${fps} fps` : (camera?.fps ? `${camera.fps.toFixed(1)} fps` : "1080p standard")}
              </span>
            </div>
          </dl>
        </div>
      </div>
    </div>
  );
}



// --- Unified General & Capabilities Panel ------------------------------------------------------

function CameraGeneral({
  cameraId,
  canManage,
  frigate,
  onApplyToFrigate,
}: {
  cameraId: string;
  canManage: boolean;
  frigate: GeneralFrigate;
  onApplyToFrigate: (patch: Record<string, unknown>) => void;
}) {
  const camera = useQuery(cameraQuery(cameraId));
  const [onvifOpen, setOnvifOpen] = useState(false);
  if (camera.isLoading) return <p className="text-sm text-muted">Cargando ajustes…</p>;
  if (camera.error || !camera.data) return <ErrorNote error={camera.error ?? new Error("No se pudo leer la cámara.")} />;

  const canEditFrigate = !frigate.ctx.readOnly;
  const stage = (section: string) => (canEditFrigate && knowsSection(frigate, section) ? section : undefined);
  const lprDraft = knowsSection(frigate, "lpr") ? sectionEnabled(frigate, "lpr") : undefined;
  const type = cameraTypeOf(camera.data.tags, {
    frigateLpr: lprDraft,
    looksProvision: isProvisionCamera(camera.data),
  });

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="flex items-center gap-2 text-lg font-semibold">
          <Icon icon={SlidersHorizontal} size="sm" className="text-primary" />
          <span>Ajustes generales</span>
        </h2>
        <Button type="button" variant="secondary" onClick={() => setOnvifOpen(true)}>
          <Icon icon={ShieldCheck} size="xs" /> Capacidades ONVIF
        </Button>
      </div>

      <CameraEnabledSwitch
        camera={camera.data}
        canManage={canManage}
        onStageFrigate={stage("enabled") ? (next) => frigate.setSection("enabled", next) : undefined}
      />
      <CameraTypeSelect
        camera={camera.data}
        current={type}
        canManage={canManage}
        onStageLpr={stage("lpr") ? (next) => frigate.setSection("lpr", setIn(frigate.valueOf("lpr"), ["enabled"], next)) : undefined}
      />

      <FrigateGeneralFields f={frigate} />

      {type === "lpr_frigate" && <LprTestCard cameraId={cameraId} engine="frigate" />}

      {/* VMS settings (name, quality, location, description, tags) */}
      <div className="border-t border-outline-variant/60 pt-6">
        <CameraGeneralForm camera={camera.data} canManage={canManage} />
      </div>

      {onvifOpen && (
        <Modal title="Capacidades ONVIF" onClose={() => setOnvifOpen(false)} className="max-w-5xl">
          <CameraOnvifPanel cameraId={cameraId} camera={camera.data} canManage={canManage} onApplyToFrigate={onApplyToFrigate} />
        </Modal>
      )}
    </div>
  );
}

const MANAGED_TAGS = new Set<string>(TYPE_TAGS);

function CameraGeneralForm({ camera, canManage }: { camera: Schemas["Camera"]; canManage: boolean }) {
  const qc = useQueryClient();
  const [name, setName] = useState(camera.display_name);
  const [quality, setQuality] = useState(camera.default_live_quality);
  const [description, setDescription] = useState(camera.description);
  const [location, setLocation] = useState(camera.location);
  // Type-managed tags (lpr/provision) are edited with the camera type selector, not here.
  const [tags, setTags] = useState(camera.tags.filter((t) => !MANAGED_TAGS.has(t.toLowerCase())).join(", "));
  const [invalid, setInvalid] = useState(false);

  // Camera Type / Variants (Nx Witness / Avigilon ACC compact toggles)
  const [isPtz, setIsPtz] = useState(camera.onvif_capabilities?.has_ptz ?? camera.tags.includes("ptz"));
  const [isFisheye, setIsFisheye] = useState(camera.tags.includes("fisheye") || camera.tags.includes("ojo_de_pez"));
  const [hasAudio, setHasAudio] = useState(camera.tags.includes("audio") || camera.tags.includes("mic"));
  const [isThermal, setIsThermal] = useState(camera.tags.includes("thermal") || camera.tags.includes("termica"));

  const save = useMutation({
    mutationFn: async () => {
      const parsedTags = new Set(tags.split(",").map((t) => t.trim().toLowerCase()).filter((t) => t && !MANAGED_TAGS.has(t)));
      if (isPtz) parsedTags.add("ptz"); else parsedTags.delete("ptz");
      if (isFisheye) parsedTags.add("fisheye"); else parsedTags.delete("fisheye");
      for (const tag of camera.tags) if (MANAGED_TAGS.has(tag.toLowerCase())) parsedTags.add(tag);
      if (hasAudio) parsedTags.add("audio"); else parsedTags.delete("audio");
      if (isThermal) parsedTags.add("thermal"); else parsedTags.delete("thermal");

      return unwrap(
        await api.PATCH("/api/v1/cameras/{cameraId}", {
          params: { path: { cameraId: camera.id } },
          body: {
            display_name: name.trim(),
            enabled: camera.enabled,
            default_live_quality: quality,
            description: description.trim(),
            location: location.trim(),
            tags: Array.from(parsedTags),
          },
        })
      );
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["cameras"] });
    },
  });

  function submit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    save.mutate();
  }

  const policy = useQuery(classifyPolicyQuery);
  const row = policy.data?.cameras.find((item) => item.id === camera.id);

  if (!canManage)
    return <p className="text-sm text-muted">Necesitás cameras.manage para editar el nombre, la ubicación y el resto de los datos de OpenVMS.</p>;

  return (
    <form onSubmit={submit} className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-semibold flex items-center gap-2">
          <Icon icon={SlidersHorizontal} size="sm" className="text-primary" />
          <span>Ajustes Generales y Funciones de Cámara</span>
        </h2>
        <p className="text-sm text-on-surface-variant">
          Configuración principal del dispositivo en el VMS y selección de variantes de hardware.
        </p>
      </div>

      {/* Nx Witness / Avigilon Camera Type & Capabilities Variants Grid */}
      <div className="flex flex-col gap-3 rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-4">
        <div className="flex flex-col gap-0.5">
          <span className="text-xs font-bold text-muted uppercase tracking-wider">
            Tipo de Cámara y Funciones Especiales
          </span>
          <p className="text-xs text-muted">
            Indica las capacidades operativas para activar los módulos de analítica y control correspondientes.
          </p>
        </div>
        <div className="grid gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
          <label
            className={cn(
              "flex items-center gap-2.5 rounded-m3-lg border p-3 cursor-pointer select-none transition-colors",
              isPtz
                ? "border-primary bg-primary/10 text-ink font-semibold shadow-xs"
                : "border-outline-variant/60 bg-surface-1 text-on-surface hover:bg-surface-3"
            )}
          >
            <input
              type="checkbox"
              checked={isPtz}
              onChange={(e) => setIsPtz(e.target.checked)}
              className="size-4 rounded accent-primary"
            />
            <div className="flex flex-col">
              <span className="text-xs font-bold flex items-center gap-1.5">
                <Icon icon={Radio} size="xs" /> PTZ Motorizado
              </span>
              <span className="text-[11px] text-muted">Control de paneo, inclinación y zoom</span>
            </div>
          </label>

          <label
            className={cn(
              "flex items-center gap-2.5 rounded-m3-lg border p-3 cursor-pointer select-none transition-colors",
              isFisheye
                ? "border-primary bg-primary/10 text-ink font-semibold shadow-xs"
                : "border-outline-variant/60 bg-surface-1 text-on-surface hover:bg-surface-3"
            )}
          >
            <input
              type="checkbox"
              checked={isFisheye}
              onChange={(e) => setIsFisheye(e.target.checked)}
              className="size-4 rounded accent-primary"
            />
            <div className="flex flex-col">
              <span className="text-xs font-bold flex items-center gap-1.5">
                <Icon icon={Eye} size="xs" /> Ojo de Pez (360°)
              </span>
              <span className="text-[11px] text-muted">Lente panorámica y dewarping</span>
            </div>
          </label>

          <label
            className={cn(
              "flex items-center gap-2.5 rounded-m3-lg border p-3 cursor-pointer select-none transition-colors",
              hasAudio
                ? "border-primary bg-primary/10 text-ink font-semibold shadow-xs"
                : "border-outline-variant/60 bg-surface-1 text-on-surface hover:bg-surface-3"
            )}
          >
            <input
              type="checkbox"
              checked={hasAudio}
              onChange={(e) => setHasAudio(e.target.checked)}
              className="size-4 rounded accent-primary"
            />
            <div className="flex flex-col">
              <span className="text-xs font-bold flex items-center gap-1.5">
                <Icon icon={Activity} size="xs" /> Audio / Micrófono
              </span>
              <span className="text-[11px] text-muted">Captura y monitoreo de sonido</span>
            </div>
          </label>

          <label
            className={cn(
              "flex items-center gap-2.5 rounded-m3-lg border p-3 cursor-pointer select-none transition-colors",
              isThermal
                ? "border-primary bg-primary/10 text-ink font-semibold shadow-xs"
                : "border-outline-variant/60 bg-surface-1 text-on-surface hover:bg-surface-3"
            )}
          >
            <input
              type="checkbox"
              checked={isThermal}
              onChange={(e) => setIsThermal(e.target.checked)}
              className="size-4 rounded accent-primary"
            />
            <div className="flex flex-col">
              <span className="text-xs font-bold flex items-center gap-1.5">
                <Icon icon={Sparkles} size="xs" /> Cámara Térmica
              </span>
              <span className="text-[11px] text-muted">Sensor infrarrojo y termografía</span>
            </div>
          </label>
        </div>
      </div>

      {/* Main Form Fields */}
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="sm:col-span-2">
          <BodyClassifySwitch scope="camera" id={camera.id} enabled={row?.enabled ?? true} />
          {row && row.enabled && !row.effective && (
            <p className="mt-1 text-xs text-muted">El servidor de esta cámara tiene la clasificación fina apagada.</p>
          )}
        </div>
        <Field label="Nombre">
          <TextInput value={name} onChange={(e) => setName(e.target.value)} aria-invalid={invalid} />
        </Field>
        {invalid && <p role="alert" className="text-xs text-bad sm:col-span-2">El nombre no puede estar vacío.</p>}
        <Field label="Calidad en vivo por defecto" hint="Flujo que usa la cámara al añadirla a la grilla de Vivo.">
          <Select value={quality} onChange={(e) => setQuality(e.target.value as Schemas["Camera"]["default_live_quality"])}>
            <option value="sub">Sub (menor calidad)</option>
            <option value="main">Main (alta calidad)</option>
          </Select>
        </Field>
        <Field label="Ubicación">
          <TextInput value={location} maxLength={200} onChange={(e) => setLocation(e.target.value)} />
        </Field>
        <Field label="Descripción">
          <TextInput value={description} maxLength={1000} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        <Field label="Etiquetas" hint="Separadas por comas (máximo 20).">
          <TextInput value={tags} onChange={(e) => setTags(e.target.value)} />
        </Field>
      </div>

      <div className="flex flex-wrap items-center justify-end gap-2">
        <ErrorNote error={save.error} />
        <Button type="submit" variant="primary" disabled={save.isPending}>
          {save.isPending ? "Guardando…" : "Guardar"}
        </Button>
      </div>
      {save.isSuccess && (
        <p role="status" className="text-xs text-ok">
          Guardado en OpenVMS. Las demás secciones se aplican en el servidor Frigate.
        </p>
      )}
    </form>
  );
}

function CameraOnvifPanel({
  cameraId,
  camera,
  canManage,
  onApplyToFrigate,
}: {
  cameraId: string;
  camera?: Schemas["Camera"];
  canManage: boolean;
  onApplyToFrigate: (patch: Record<string, unknown>) => void;
}) {
  const qc = useQueryClient();
  const [endpointInput, setEndpointInput] = useState(camera?.onvif_endpoint ?? "");
  const [usernameInput, setUsernameInput] = useState("");
  const [passwordInput, setPasswordInput] = useState("");
  const [applySuccessMsg, setApplySuccessMsg] = useState<string | null>(null);

  const checkMutation = useMutation({
    mutationFn: async () => {
      setApplySuccessMsg(null);
      return unwrap(
        await api.POST("/api/v1/cameras/{cameraId}/onvif/check", {
          params: { path: { cameraId } },
          body: {
            endpoint: endpointInput.trim() || undefined,
            username: usernameInput || undefined,
            password: passwordInput || undefined,
          },
        })
      );
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["cameras"] });
      await qc.invalidateQueries({ queryKey: ["cameras", cameraId, "one"] });
    },
  });

  const [syncTimeResult, setSyncTimeResult] = useState<string | null>(null);
  const syncTimeMutation = useMutation({
    mutationFn: async () => {
      setSyncTimeResult(null);
      return unwrap(
        await api.POST("/api/v1/cameras/{cameraId}/onvif/sync-time", {
          params: { path: { cameraId } },
          body: {
            endpoint: endpointInput.trim() || undefined,
            username: usernameInput || undefined,
            password: passwordInput || undefined,
          },
        })
      );
    },
    onSuccess: (res) => {
      setSyncTimeResult(`Hora sincronizada correctamente con el servidor (${new Date(res.synced_at).toLocaleString()}).`);
      void qc.invalidateQueries({ queryKey: ["cameras"] });
    },
  });

  const [ptzStatus, setPtzStatus] = useState<string | null>(null);
  const ptzMutation = useMutation({
    mutationFn: async (args: { action: "continuous_move" | "stop" | "goto_preset"; pan?: number; tilt?: number; zoom?: number; presetToken?: string }) => {
      setPtzStatus(null);
      return unwrap(
        await api.POST("/api/v1/cameras/{cameraId}/onvif/ptz", {
          params: { path: { cameraId } },
          body: {
            action: args.action,
            endpoint: endpointInput.trim() || undefined,
            username: usernameInput || undefined,
            password: passwordInput || undefined,
            pan: args.pan,
            tilt: args.tilt,
            zoom: args.zoom,
            preset_token: args.presetToken,
          },
        })
      );
    },
    onSuccess: () => {
      setPtzStatus("Comando PTZ ejecutado correctamente.");
    },
  });

  const caps = camera?.onvif_capabilities;
  const detectedHost = camera?.onvif_endpoint ? displayEndpointHost(camera.onvif_endpoint) : "";

  const handleApplyToFrigate = () => {
    if (!detectedHost) return;
    onApplyToFrigate({
      host: detectedHost,
      ...(usernameInput ? { user: usernameInput } : {}),
      ...(passwordInput ? { password: passwordInput } : {}),
    });
    setApplySuccessMsg(`Host "${detectedHost}" cargado en la pestaña ONVIF de Frigate. Podes revisar y aplicar los cambios.`);
  };

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-semibold flex items-center gap-2">
          <Icon icon={ShieldCheck} size="sm" className="text-primary" />
          <span>Soporte y Capacidades ONVIF</span>
        </h2>
        <p className="text-sm text-on-surface-variant">
          Información detectada directamente desde la cámara mediante el protocolo estándar ONVIF (Profile S/T/M).
        </p>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <div className="flex flex-col gap-1 rounded-m3-lg bg-surface-2 p-3.5 border border-outline-variant/40">
          <span className="text-xs text-muted font-medium">Perfil y Versión</span>
          <div className="flex items-center gap-2">
            <span className="font-semibold text-ink">{camera?.onvif_profile ?? "No detectado"}</span>
            {camera?.onvif_version && <span className="font-mono text-xs text-muted">v{camera.onvif_version}</span>}
          </div>
          {caps?.profiles && caps.profiles.length > 0 && (
            <span className="text-xs text-muted">{caps.profiles.join(", ")}</span>
          )}
        </div>

        <div className="flex flex-col gap-1 rounded-m3-lg bg-surface-2 p-3.5 border border-outline-variant/40">
          <span className="text-xs text-muted font-medium">Dispositivo</span>
          <span className="font-semibold text-ink">
            {caps?.manufacturer || caps?.model ? `${caps?.manufacturer ?? ""} ${caps?.model ?? ""}`.trim() : "—"}
          </span>
          <span className="font-mono text-xs text-muted">
            {caps?.firmware_version ? `FW: ${caps.firmware_version}` : (caps?.serial_number ? `SN: ${caps.serial_number}` : "—")}
          </span>
        </div>

        <div className="flex flex-col gap-1 rounded-m3-lg bg-surface-2 p-3.5 border border-outline-variant/40">
          <span className="text-xs text-muted font-medium">Endpoint ONVIF</span>
          <span className="break-all font-mono text-xs text-ink">{camera?.onvif_endpoint ?? "No vinculado"}</span>
        </div>
      </div>

      <div className="flex flex-col gap-3 rounded-m3-lg bg-surface-2 p-4 border border-outline-variant/40">
        <h3 className="text-sm font-semibold text-ink">Funcionalidades de la Cámara</h3>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="flex items-start gap-3 rounded-m3-md bg-surface-1 p-3">
            <div className={cn("mt-1 size-2.5 shrink-0 rounded-full", caps?.has_analytics ? "bg-ok" : "bg-muted/40")} />
            <div className="flex flex-col gap-0.5">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">Detecciones y Analítica</span>
                <span className={cn("rounded-m3-sm px-1.5 py-0.5 text-[10px] font-semibold", caps?.has_analytics ? "bg-ok/12 text-ok" : "bg-muted/12 text-muted")}>
                  {caps?.has_analytics ? "Soportado (Profile M)" : "No detectado"}
                </span>
              </div>
              <p className="text-xs text-muted">
                {caps?.has_analytics
                  ? "La cámara procesa analítica y metadatos de detección en hardware (WSDL ver20/analytics)."
                  : "No se encontró servicio de analítica ONVIF reportado en el descriptor WSDL."}
              </p>
            </div>
          </div>

          <div className="flex items-start gap-3 rounded-m3-md bg-surface-1 p-3">
            <div className={cn("mt-1 size-2.5 shrink-0 rounded-full", caps?.has_events ? "bg-ok" : "bg-muted/40")} />
            <div className="flex flex-col gap-0.5">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">Eventos y Notificaciones</span>
                <span className={cn("rounded-m3-sm px-1.5 py-0.5 text-[10px] font-semibold", caps?.has_events ? "bg-ok/12 text-ok" : "bg-muted/12 text-muted")}>
                  {caps?.has_events ? "Soportado" : "No detectado"}
                </span>
              </div>
              <p className="text-xs text-muted">
                {caps?.has_events
                  ? "Soporte de alertas de movimiento, sabotaje y eventos push vía WS-BaseNotification."
                  : "No reporta servicio de eventos ONVIF (ver10/events)."}
              </p>
            </div>
          </div>

          <div className="flex items-start gap-3 rounded-m3-md bg-surface-1 p-3">
            <div className={cn("mt-1 size-2.5 shrink-0 rounded-full", caps?.has_ptz ? "bg-ok" : "bg-muted/40")} />
            <div className="flex flex-col gap-0.5">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">Control PTZ</span>
                <span className={cn("rounded-m3-sm px-1.5 py-0.5 text-[10px] font-semibold", caps?.has_ptz ? "bg-ok/12 text-ok" : "bg-muted/12 text-muted")}>
                  {caps?.has_ptz ? "Soportado" : "No detectado"}
                </span>
              </div>
              <p className="text-xs text-muted">
                {caps?.has_ptz
                  ? "Permite control motorizado de paneo, inclinación, zoom y presets."
                  : "Cámara fija o sin servicio PTZ ONVIF habilitado."}
              </p>
            </div>
          </div>

          <div className="flex items-start gap-3 rounded-m3-md bg-surface-1 p-3">
            <div className={cn("mt-1 size-2.5 shrink-0 rounded-full", caps?.has_media2 ? "bg-ok" : "bg-muted/40")} />
            <div className="flex flex-col gap-0.5">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">Streaming Media 2 (Profile T)</span>
                <span className={cn("rounded-m3-sm px-1.5 py-0.5 text-[10px] font-semibold", caps?.has_media2 ? "bg-ok/12 text-ok" : "bg-muted/12 text-muted")}>
                  {caps?.has_media2 ? "Soportado (H.265)" : "Media 1 (H.264)"}
                </span>
              </div>
              <p className="text-xs text-muted">
                {caps?.has_media2
                  ? "Compatible con códec H.265 y perfiles de streaming de alta eficiencia."
                  : "Utiliza protocolo de medios v1.0 (Profile S)."}
              </p>
            </div>
          </div>
        </div>
      </div>

      <AdvertisedAnalytics analytics={caps?.analytics} />

      {caps?.services && caps.services.length > 0 && (
        <div className="flex flex-col gap-2">
          <span className="text-xs font-semibold text-muted uppercase">Servicios WSDL detectados ({caps.services.length})</span>
          <div className="flex flex-wrap gap-1.5">
            {caps.services.map((s, idx) => (
              <span key={idx} className="rounded-m3-sm bg-surface-2 px-2 py-1 font-mono text-[11px] text-on-surface-variant border border-outline-variant/30">
                {s.namespace.split("/").pop() || s.namespace} · v{s.version.major}.{s.version.minor}
              </span>
            ))}
          </div>
        </div>
      )}

      {detectedHost && (
        <div className="flex flex-col gap-2 rounded-m3-lg border border-primary/20 bg-primary/4 p-4">
          <div className="flex items-center justify-between gap-3">
            <div>
              <p className="text-sm font-semibold text-ink">Aplicar host a la configuración de Frigate</p>
              <p className="text-xs text-on-surface-variant">
                Carga automáticamente el host detectado (<code className="font-mono">{detectedHost}</code>) en la pestaña ONVIF de Frigate para vincular autotracking y control.
              </p>
            </div>
            <button
              type="button"
              className="shrink-0 rounded-m3-full bg-primary px-4 py-2 text-xs font-medium text-on-primary hover:bg-primary/90"
              onClick={handleApplyToFrigate}
            >
              Cargar en Frigate
            </button>
          </div>
          {applySuccessMsg && <p role="status" className="text-xs font-medium text-ok">{applySuccessMsg}</p>}
        </div>
      )}

      <div className="flex flex-col gap-3 rounded-m3-lg bg-surface-2 p-4 border border-outline-variant/40">
        <h3 className="text-sm font-semibold text-ink">Comprobar o actualizar ONVIF de esta cámara</h3>
        <p className="text-xs text-on-surface-variant">
          Consulta la cámara desde el servidor a través de la red local para verificar su versión y capacidades de analítica.
        </p>
        <div className="grid gap-3 sm:grid-cols-3">
          <Field label="Endpoint ONVIF" hint="Si se deja vacío, se detecta de la IP del stream.">
            <TextInput
              value={endpointInput}
              onChange={(e) => setEndpointInput(e.target.value)}
              placeholder={camera?.onvif_endpoint || "http://10.1.1.X/onvif/device_service"}
            />
          </Field>
          <Field label="Usuario ONVIF (opcional)" hint="Por defecto usa el usuario del stream si se deja vacío.">
            <TextInput
              value={usernameInput}
              onChange={(e) => setUsernameInput(e.target.value)}
              placeholder="admin"
            />
          </Field>
          <Field label="Contraseña ONVIF (opcional)" hint="Por defecto usa la contraseña del stream si se deja vacía.">
            <TextInput
              type="password"
              value={passwordInput}
              onChange={(e) => setPasswordInput(e.target.value)}
              placeholder="••••••••"
            />
          </Field>
        </div>
        <div className="flex items-center justify-between gap-2 pt-1">
          <ErrorNote error={checkMutation.error} />
          {checkMutation.isSuccess && (
            <p role="status" className="text-xs font-medium text-ok">
              ¡Capacidades ONVIF comprobadas y actualizadas correctamente!
            </p>
          )}
          <Button
            type="button"
            variant="primary"
            disabled={checkMutation.isPending || !canManage}
            onClick={() => checkMutation.mutate()}
          >
            {checkMutation.isPending ? "Comprobando…" : "Comprobar y guardar ONVIF"}
          </Button>
        </div>
      </div>

      {/* Sincronización de Fecha y Hora */}
      <div className="flex flex-col gap-2 rounded-m3-lg bg-surface-2 p-4 border border-outline-variant/40">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold text-ink flex items-center gap-1.5">
              <Icon icon={Clock} size="xs" />
              <span>Sincronización de Fecha y Hora con el Servidor</span>
            </h3>
            <p className="text-xs text-on-surface-variant">
              Ajusta el reloj interno de la cámara a la hora UTC exacta del servidor para prevenir errores de autenticación Digest y desfasajes en eventos.
            </p>
          </div>
          <Button
            type="button"
            variant="secondary"
            disabled={syncTimeMutation.isPending || !canManage}
            onClick={() => syncTimeMutation.mutate()}
          >
            {syncTimeMutation.isPending ? "Sincronizando…" : "Sincronizar fecha y hora"}
          </Button>
        </div>
        <ErrorNote error={syncTimeMutation.error} />
        {syncTimeResult && <p role="status" className="text-xs font-medium text-ok">{syncTimeResult}</p>}
      </div>

      {/* Controles PTZ Directos */}
      {caps?.has_ptz && (
        <div className="flex flex-col gap-3 rounded-m3-lg bg-surface-2 p-4 border border-outline-variant/40">
          <h3 className="text-sm font-semibold text-ink flex items-center gap-1.5">
            <Icon icon={Compass} size="xs" />
            <span>Control PTZ Directo (ONVIF)</span>
          </h3>
          <p className="text-xs text-on-surface-variant">
            Control de movimiento panorámico, inclinación y zoom a través del protocolo ONVIF estándar.
          </p>
          <div className="flex flex-wrap items-center gap-4">
            <div className="grid grid-cols-3 gap-1 w-32">
              <div />
              <button
                type="button"
                className="h-9 rounded-m3-md border border-outline-variant bg-surface-1 font-bold text-xs hover:bg-surface-3 active:bg-primary active:text-on-primary"
                onClick={() => ptzMutation.mutate({ action: "continuous_move", tilt: 0.5 })}
                title="Arriba"
              >
                ▲
              </button>
              <div />
              <button
                type="button"
                className="h-9 rounded-m3-md border border-outline-variant bg-surface-1 font-bold text-xs hover:bg-surface-3 active:bg-primary active:text-on-primary"
                onClick={() => ptzMutation.mutate({ action: "continuous_move", pan: -0.5 })}
                title="Izquierda"
              >
                ◀
              </button>
              <button
                type="button"
                className="h-9 rounded-m3-md border border-outline-variant bg-surface-1 font-bold text-xs hover:bg-bad/20 text-bad"
                onClick={() => ptzMutation.mutate({ action: "stop" })}
                title="Detener"
              >
                ■
              </button>
              <button
                type="button"
                className="h-9 rounded-m3-md border border-outline-variant bg-surface-1 font-bold text-xs hover:bg-surface-3 active:bg-primary active:text-on-primary"
                onClick={() => ptzMutation.mutate({ action: "continuous_move", pan: 0.5 })}
                title="Derecha"
              >
                ▶
              </button>
              <div />
              <button
                type="button"
                className="h-9 rounded-m3-md border border-outline-variant bg-surface-1 font-bold text-xs hover:bg-surface-3 active:bg-primary active:text-on-primary"
                onClick={() => ptzMutation.mutate({ action: "continuous_move", tilt: -0.5 })}
                title="Abajo"
              >
                ▼
              </button>
              <div />
            </div>

            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  className="rounded-m3-md border border-outline-variant bg-surface-1 px-3 py-1.5 text-xs font-medium hover:bg-surface-3"
                  onClick={() => ptzMutation.mutate({ action: "continuous_move", zoom: 0.5 })}
                >
                  Zoom +
                </button>
                <button
                  type="button"
                  className="rounded-m3-md border border-outline-variant bg-surface-1 px-3 py-1.5 text-xs font-medium hover:bg-surface-3"
                  onClick={() => ptzMutation.mutate({ action: "continuous_move", zoom: -0.5 })}
                >
                  Zoom -
                </button>
                <button
                  type="button"
                  className="rounded-m3-md border border-bad/30 bg-bad/10 px-3 py-1.5 text-xs font-medium text-bad hover:bg-bad/20"
                  onClick={() => ptzMutation.mutate({ action: "stop" })}
                >
                  Detener
                </button>
              </div>
              {ptzStatus && <p role="status" className="text-xs text-ok">{ptzStatus}</p>}
              <ErrorNote error={ptzMutation.error} />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function LiveBadge({ live }: { live: boolean }) {
  return live ? (
    <span className="text-[10px] tracking-wide text-ok uppercase">En vivo</span>
  ) : (
    <span className="text-[10px] tracking-wide text-warn uppercase">Requiere reinicio</span>
  );
}

function DiffList({
  sections,
  before,
  after,
}: {
  sections: string[];
  before: Record<string, unknown>;
  after: Record<string, unknown>;
}) {
  return (
    <div className="flex flex-col gap-4 text-sm">
      {sections.map((s) => (
        <div key={s}>
          <h3 className="mb-1 flex items-center justify-between font-medium">
            {sectionLabel(s)}
            <LiveBadge live={isLiveSection(s)} />
          </h3>
          <ul className="flex flex-col gap-1 rounded-m3-lg bg-surface-2 p-3">
            {diffValues(before[s], after[s], [s]).map((c) => (
              <li key={c.path.join(".")} className="break-words">
                <span className="font-mono text-xs text-muted">{pathLabel(c.path)}</span>{" "}
                <DiffValue value={c.before} className="text-bad line-through" />
                {" → "}
                <DiffValue value={c.after} className="text-ok" />
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}

const DIFF_MAX = 120;

function DiffValue({ value, className }: { value: unknown; className: string }) {
  const text = formatValue(value);
  if (text.length <= DIFF_MAX) return <span className={className}>{text}</span>;
  return (
    <details className="inline align-top">
      <summary className={cn("inline cursor-pointer", className)}>
        {text.slice(0, DIFF_MAX)}… ({text.length} caracteres)
      </summary>
      <code className={cn("mt-1 block font-mono text-xs break-all whitespace-pre-wrap", className)}>{text}</code>
    </details>
  );
}

function ResultList({ results }: { results: SectionResult[] }) {
  return (
    <ul aria-label="Resultado de los cambios" className="flex flex-col gap-1 rounded-m3-lg bg-surface-1 px-4 py-3 text-sm">
      {results.map((r) => (
        <li key={r.section} className="flex items-center gap-2">
          <Icon icon={r.requires_restart ? RefreshCw : CircleCheck} size="xs" className={r.requires_restart ? "text-warn" : "text-ok"} />
          <span className="font-medium">{sectionLabel(r.section)}</span>
          <span className="text-muted">{r.applied_live ? "aplicado en vivo" : r.requires_restart ? "guardado, requiere reinicio" : "guardado"}</span>
        </li>
      ))}
    </ul>
  );
}

function RestartBanner({ serverId, canRestart, onDone }: { serverId: string; canRestart: boolean; onDone: () => void }) {
  const [confirming, setConfirming] = useState(false);
  const restart = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/servers/{serverId}/restart", { params: { path: { serverId } } })),
    onSuccess: () => {
      setConfirming(false);
      onDone();
    },
  });
  return (
    <div role="status" className="flex flex-wrap items-center justify-between gap-3 rounded-m3-xl bg-warn/10 px-5 py-3 text-sm">
      <span className="flex items-center gap-2 text-warn">
        <Icon icon={TriangleAlert} size="xs" /> Hay cambios que requieren reiniciar el servidor.
      </span>
      {canRestart ? (
        <Button size="sm" onClick={() => setConfirming(true)}>Reiniciar servidor</Button>
      ) : (
        <span className="text-xs text-muted">Pide a un administrador con permiso servers.restart que reinicie el servidor.</span>
      )}
      {confirming && (
        <ConfirmDialog
          title="Reiniciar servidor"
          message="Frigate se reiniciará y la grabación y la detección se pausarán unos instantes en todas sus cámaras. ¿Continuar?"
          confirmLabel="Reiniciar"
          pending={restart.isPending}
          error={restart.error}
          onConfirm={() => restart.mutate()}
          onCancel={() => setConfirming(false)}
        />
      )}
    </div>
  );
}
