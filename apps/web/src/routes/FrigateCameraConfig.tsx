import { useT } from "@/i18n";
import { faArrowLeft, faArrowsRotate, faCircleCheck, faTriangleExclamation } from "@fortawesome/free-solid-svg-icons";
import { FontAwesomeIcon } from "@fortawesome/react-fontawesome";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { type FormEvent, useMemo, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { cameraFrigateDocQuery, classifyPolicyQuery, frigateSchemaQuery, meQuery } from "@/api/queries";
import { BodyClassifySwitch } from "@/components/BodyClassifySwitch";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { FrigateHistory } from "@/components/frigate/FrigateHistory";
import { LabelPicker } from "@/components/frigate/LabelPicker";
import { SectionPanel } from "@/components/frigate/SectionPanel";
import { Modal } from "@/components/Modal";
import { ZoneEditorModal, type ZoneEditorValue } from "@/components/zones/ZoneEditorModal";
import { Button, ErrorNote, Field, PageHeader, Select, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import {
  buildPatch, deepEqual, diffValues, formatValue, isLiveSection, type JSchema, kindOf, orderSections, parseVersion, pathLabel, resolve, schemaAt,
  sectionLabel, validateTree,
} from "@/lib/frigateSchema";
import { pickerLabels } from "@/lib/labelEmoji";
import { can } from "@/lib/perm";

type SectionResult = Schemas["FrigateSectionResult"];
const HISTORY = "__history";
const GENERAL = "__general";

const isRec = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const isEmptyMask = (v: unknown) => v == null || v === "" || (Array.isArray(v) && v.length === 0) || (isRec(v) && Object.keys(v).length === 0);

/**
 * withMask returns `obj` with its `mask` set to `next`. A cleared mask keeps the empty shape of
 * what the server holds (so the patch clears it) or is dropped when the server never had one.
 * The same object is returned when nothing changes, so untouched sections stay clean.
 */
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

const restartKey = (serverId: string) => `openvms.frigate-restart.${serverId}`;
function loadRestart(serverId: string): boolean {
  try {
    return sessionStorage.getItem(restartKey(serverId)) === "1";
  } catch {
    return false;
  }
}
function saveRestart(serverId: string, on: boolean) {
  try {
    if (on) sessionStorage.setItem(restartKey(serverId), "1");
    else sessionStorage.removeItem(restartKey(serverId));
  } catch {
    /* storage unavailable: the banner just does not survive a reload */
  }
}

export function FrigateCameraConfig() {
  const t = useT();
  const { cameraId } = useParams({ strict: false }) as { cameraId: string };
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const doc = useQuery(cameraFrigateDocQuery(cameraId));
  const serverId = doc.data?.server_id;
  const schema = useQuery(frigateSchemaQuery(serverId));
  const [active, setActive] = useState<string>();
  const [edits, setEdits] = useState<Record<string, unknown>>({});
  const [reviewing, setReviewing] = useState(false);
  const [results, setResults] = useState<SectionResult[]>([]);
  const [zoneEditor, setZoneEditor] = useState(false);
  const [restartPending, setRestartPending] = useState<Record<string, boolean>>({});

  const root = schema.data as JSchema | undefined;
  const config = useMemo(() => (doc.data?.config ?? {}) as Record<string, unknown>, [doc.data]);
  const camProps = useMemo(() => (root ? (resolve(root, root).node.properties ?? {}) : {}), [root]);
  const sections = useMemo(() => orderSections([...Object.keys(camProps), ...Object.keys(config)]), [camProps, config]);
  const version = doc.data?.frigate_version ?? "";
  const pv = parseVersion(version);
  const versionOk = !pv || pv[0] > 0 || pv[1] >= 16;
  const readOnly = !doc.data?.editable;
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [root, camProps, edits, config]);
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
      // Earlier sections may have been applied before the failure: show what Frigate now holds.
      await qc.invalidateQueries({ queryKey: ["cameras", cameraId] });
    },
  });

  const activeSection = active ?? sections[0];

  const zoneEditorValue = (): ZoneEditorValue => {
    const objects = (valueOf("objects") ?? {}) as Record<string, unknown>;
    const filters = (isRec(objects.filters) ? objects.filters : {}) as Record<string, Record<string, unknown>>;
    const filterMasks: Record<string, unknown> = {};
    for (const [label, f] of Object.entries(filters)) if (isRec(f) && f.mask !== undefined) filterMasks[label] = f.mask;
    return {
      zones: (isRec(valueOf("zones")) ? valueOf("zones") : {}) as ZoneEditorValue["zones"],
      motionMask: (valueOf("motion") as Record<string, unknown> | undefined)?.mask,
      objectMask: objects.mask,
      objectFilterMasks: filterMasks,
    };
  };

  /** Folds the modal result into the draft; the normal review/apply flow sends it. Removed zones become null in the patch (map deletion). */
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
    setZoneEditor(false);
  };
  const detectDraft = valueOf("detect") as Record<string, unknown> | undefined;
  const objectsDraft = valueOf("objects") as Record<string, unknown> | undefined;
  const zoneLabels = pickerLabels(Object.keys(isRec(objectsDraft?.filters) ? objectsDraft.filters : {}), Array.isArray(objectsDraft?.track) ? (objectsDraft.track as string[]) : []);
  const ctx = { root: root ?? {}, secretsVisible, readOnly };
  const header = (
    <PageHeader
      title={`Frigate · ${doc.data?.camera_name ?? t("Cámara")}`}
      description={version ? t("Configuración de la cámara en Frigate {version}. Los cambios se aplican al servidor.", { version }) : undefined}
      actions={
        <Link to="/cameras" className="inline-flex items-center gap-2 text-sm text-muted hover:text-ink">
          <FontAwesomeIcon icon={faArrowLeft} aria-hidden /> Volver a cámaras
        </Link>
      }
    />
  );

  if (doc.isLoading || (serverId && schema.isLoading)) {
    return <div className="mx-auto max-w-6xl">{header}<p className="mt-6 text-sm text-muted">Cargando configuración de Frigate…</p></div>;
  }
  if (doc.error || schema.error || !doc.data || !root) {
    return <div className="mx-auto flex max-w-6xl flex-col gap-4">{header}<ErrorNote error={doc.error ?? schema.error ?? new Error("Configuración de Frigate no disponible.")} /></div>;
  }

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-4">
      {needsRestart && (
        <RestartBanner serverId={doc.data.server_id} canRestart={canRestart} onDone={() => setNeedsRestart(false)} />
      )}
      {readOnly && (
        <p role="status" className="rounded border border-warn/40 bg-warn/10 px-3 py-2 text-sm text-warn">
          {versionOk ? "Necesitas el permiso servers.config para editar esta configuración. Solo lectura." : "Esta versión de Frigate no permite editar desde OpenVMS (requiere 0.16 o superior)."}
        </p>
      )}
      {!secretsVisible && !readOnly && (
        <p className="text-xs text-muted">Las credenciales (rutas de streams, usuario y contraseña ONVIF) están ocultas: requieren permiso de credenciales.</p>
      )}
      {results.length > 0 && <ResultList results={results} />}
      <ErrorNote
        error={apply.error ? new Error(`${apply.error.message} Las secciones anteriores pudieron aplicarse: revisa el historial o recarga para ver el estado actual.`) : null}
      />

      <CameraIdentity name={doc.data.camera_name} version={version} config={config} />
      <div className="flex flex-col gap-3">
        <nav aria-label="Secciones de configuración" className="flex flex-wrap gap-1 rounded-xl bg-bg p-1">
          <button
            type="button"
            aria-current={activeSection === GENERAL ? "page" : undefined}
            onClick={() => setActive(GENERAL)}
            className={cn("rounded-lg px-3 py-1.5 text-xs font-medium whitespace-nowrap", activeSection === GENERAL ? "bg-surface text-ink shadow-sm" : "text-muted hover:text-ink")}
          >
            {t("General")}
          </button>
          {sections.map((s) => (
            <button
              key={s}
              type="button"
              aria-current={activeSection === s ? "page" : undefined}
              onClick={() => setActive(s)}
              className={cn("rounded-lg px-3 py-1.5 text-left text-xs font-medium whitespace-nowrap", activeSection === s ? "bg-surface text-ink shadow-sm" : "text-muted hover:text-ink")}
            >
              <span className="flex flex-col">
                <span>{sectionLabel(s)}{dirty.includes(s) && <span aria-label="con cambios" className="ml-1 text-accent">●</span>}</span>
                <LiveBadge live={isLiveSection(s, version)} />
              </span>
            </button>
          ))}
          <button
            type="button"
            aria-current={activeSection === HISTORY ? "page" : undefined}
            onClick={() => setActive(HISTORY)}
            className={cn("rounded-lg px-3 py-1.5 text-left text-xs font-medium whitespace-nowrap", activeSection === HISTORY ? "bg-surface text-ink shadow-sm" : "text-muted hover:text-ink")}
          >
            Historial
          </button>
        </nav>

        <section aria-label={activeSection === HISTORY ? "Historial" : activeSection === GENERAL ? t("General") : sectionLabel(activeSection ?? "")} className="flex min-w-0 flex-col gap-4 rounded border border-line bg-surface p-4">
          {activeSection === GENERAL ? (
            <CameraGeneral cameraId={cameraId} canManage={can(me.data, "cameras.manage")} />
          ) : activeSection === HISTORY ? (
            <FrigateHistory serverId={doc.data.server_id} cameraId={cameraId} secretsVisible={secretsVisible} canRollback={!readOnly && secretsVisible} onRestored={() => setNeedsRestart(true)} />
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
          {activeSection !== HISTORY && activeSection !== GENERAL && Object.entries(errors).filter(([p]) => p.split(".")[0] === activeSection).length > 0 && (
            <p role="alert" className="text-xs text-bad">Hay valores no válidos en esta sección.</p>
          )}
        </section>
      </div>

      {!readOnly && (
        <div className="sticky bottom-0 flex flex-wrap items-center justify-between gap-2 rounded border border-line bg-surface px-4 py-3">
          <span role="status" className="text-sm text-muted">
            {dirty.length ? `${dirty.length} ${dirty.length === 1 ? "sección modificada" : "secciones modificadas"}` : "Sin cambios pendientes"}
            {errorCount > 0 && <span className="text-bad"> · {errorCount} {errorCount === 1 ? "error" : "errores"}</span>}
          </span>
          <div className="flex gap-2">
            <Button disabled={!dirty.length} onClick={() => { setEdits({}); apply.reset(); }}>Descartar</Button>
            <Button variant="primary" disabled={!dirty.length || errorCount > 0} onClick={() => setReviewing(true)}>Revisar cambios</Button>
          </div>
        </div>
      )}

      {zoneEditor && (
        <ZoneEditorModal
          open
          onClose={() => setZoneEditor(false)}
          cameraId={cameraId}
          snapshotUrl={`/media/v1/cameras/${cameraId}/snapshot.jpg`}
          frameWidth={typeof detectDraft?.width === "number" ? detectDraft.width : undefined}
          frameHeight={typeof detectDraft?.height === "number" ? detectDraft.height : undefined}
          value={zoneEditorValue()}
          frigateVersion={version}
          onSave={saveZoneEditor}
          renderLabelPicker={({ value, onChange, ariaLabel }) => <LabelPicker label={ariaLabel} value={value} onChange={onChange} />}
          availableLabels={zoneLabels}
        />
      )}

      {reviewing && (
        <Modal title="Revisar cambios" onClose={() => setReviewing(false)}>
          <DiffList sections={dirty} before={config} after={edits} version={version} />
          <ErrorNote error={apply.error} />
          <div className="flex justify-end gap-2">
            <Button onClick={() => setReviewing(false)}>Volver</Button>
            <Button variant="primary" disabled={apply.isPending} onClick={() => apply.mutate()}>
              {apply.isPending ? "Aplicando…" : "Aplicar"}
            </Button>
          </div>
        </Modal>
      )}
    </div>
  );
}

function CameraIdentity({ name, version, config }: { name: string; version: string; config: Record<string, unknown> }) {
  const detect = isRec(config.detect) ? config.detect : undefined;
  const width = typeof detect?.width === "number" ? detect.width : undefined;
  const height = typeof detect?.height === "number" ? detect.height : undefined;
  const onvif = isRec(config.onvif) ? config.onvif : undefined;
  const host = typeof onvif?.host === "string" && onvif.host && !onvif.host.includes("*") ? onvif.host : undefined;
  return (
    <div className="flex flex-col gap-3 border-b border-line pb-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight text-ink">{name}</h1>
          <p className="text-sm text-muted">Frigate {version || "—"}</p>
        </div>
        <Link to="/cameras" className="inline-flex items-center gap-2 text-sm text-muted hover:text-ink">
          <FontAwesomeIcon icon={faArrowLeft} aria-hidden /> Volver a cámaras
        </Link>
      </div>
      <dl className="grid grid-cols-2 gap-x-6 gap-y-1 text-xs sm:grid-cols-3">
        <div><dt className="text-muted">Versión</dt><dd className="font-mono">{version || "—"}</dd></div>
        <div><dt className="text-muted">Resolución</dt><dd className="font-mono">{width && height ? `${width}×${height}` : "—"}</dd></div>
        <div><dt className="text-muted">Host</dt><dd className="font-mono">{host ?? "—"}</dd></div>
      </dl>
    </div>
  );
}

function CameraGeneral({ cameraId, canManage }: { cameraId: string; canManage: boolean }) {
  const camera = useQuery({
    queryKey: ["cameras", cameraId, "one"],
    queryFn: async () => unwrap(await api.GET("/api/v1/cameras/{cameraId}", { params: { path: { cameraId } } })),
    retry: false,
  });
  if (camera.isLoading) return <p className="text-sm text-muted">Cargando ajustes…</p>;
  if (camera.error || !camera.data) return <ErrorNote error={camera.error ?? new Error("No se pudo leer la cámara.")} />;
  return <CameraGeneralForm camera={camera.data} canManage={canManage} />;
}

function CameraGeneralForm({ camera, canManage }: { camera: Schemas["Camera"]; canManage: boolean }) {
  const qc = useQueryClient();
  const [name, setName] = useState(camera.display_name);
  const [enabled, setEnabled] = useState(camera.enabled);
  const [quality, setQuality] = useState(camera.default_live_quality);
  const [description, setDescription] = useState(camera.description);
  const [location, setLocation] = useState(camera.location);
  const [tags, setTags] = useState(camera.tags.join(", "));
  const [invalid, setInvalid] = useState(false);
  const save = useMutation({
    mutationFn: async () =>
      unwrap(await api.PATCH("/api/v1/cameras/{cameraId}", {
        params: { path: { cameraId: camera.id } },
        body: {
          display_name: name.trim(),
          enabled,
          default_live_quality: quality,
          description: description.trim(),
          location: location.trim(),
          tags: tags.split(",").map((tag) => tag.trim()).filter(Boolean),
        },
      })),
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
  if (!canManage) return <p className="text-sm text-muted">Necesitás cameras.manage para editar el nombre, la ubicación y el resto de los datos de OpenVMS.</p>;
  return (
    <form onSubmit={submit} className="grid gap-3 sm:grid-cols-2">
      <div className="sm:col-span-2">
        <BodyClassifySwitch scope="camera" id={camera.id} enabled={row?.enabled ?? true} />
        {row && row.enabled && !row.effective && (
          <p className="mt-1 text-xs text-muted">El servidor de esta cámara tiene la clasificación fina apagada.</p>
        )}
      </div>
      <Field label="Nombre">
        <TextInput value={name} onChange={(e) => setName(e.target.value)} aria-invalid={invalid} />
      </Field>
      <label className="flex items-center gap-2 self-end pb-2 text-sm">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        Habilitada
      </label>
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
      <div className="flex items-center justify-end gap-2 sm:col-span-2">
        <ErrorNote error={save.error} />
        <Button type="submit" variant="primary" disabled={save.isPending}>{save.isPending ? "Guardando…" : "Guardar"}</Button>
      </div>
      {save.isSuccess && <p role="status" className="text-xs text-ok sm:col-span-2">Guardado en OpenVMS. Las demás pestañas se aplican en el servidor Frigate.</p>}
    </form>
  );
}

function LiveBadge({ live }: { live: boolean }) {
  return live ? (
    <span className="text-[10px] tracking-wide text-ok uppercase">En vivo</span>
  ) : (
    <span className="text-[10px] tracking-wide text-warn uppercase">Requiere reinicio</span>
  );
}

function DiffList({ sections, before, after, version }: { sections: string[]; before: Record<string, unknown>; after: Record<string, unknown>; version: string }) {
  return (
    <div className="flex flex-col gap-4 text-sm">
      {sections.map((s) => (
        <div key={s}>
          <h3 className="mb-1 flex items-center justify-between font-medium">
            {sectionLabel(s)}
            <LiveBadge live={isLiveSection(s, version)} />
          </h3>
          <ul className="flex flex-col gap-1 rounded border border-line p-2">
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

/** DiffValue shortens very long values (polygon coordinates, mask maps) behind an expandable detail. */
function DiffValue({ value, className }: { value: unknown; className: string }) {
  const text = formatValue(value);
  if (text.length <= DIFF_MAX) return <span className={className}>{text}</span>;
  return (
    <details className="inline align-top">
      <summary className={cn("inline cursor-pointer", className)}>{text.slice(0, DIFF_MAX)}… ({text.length} caracteres)</summary>
      <code className={cn("mt-1 block font-mono text-xs break-all whitespace-pre-wrap", className)}>{text}</code>
    </details>
  );
}

function ResultList({ results }: { results: SectionResult[] }) {
  return (
    <ul aria-label="Resultado de los cambios" className="flex flex-col gap-1 rounded border border-line bg-surface px-3 py-2 text-sm">
      {results.map((r) => (
        <li key={r.section} className="flex items-center gap-2">
          <FontAwesomeIcon icon={r.requires_restart ? faArrowsRotate : faCircleCheck} className={r.requires_restart ? "text-warn" : "text-ok"} aria-hidden />
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
    <div role="status" className="flex flex-wrap items-center justify-between gap-3 rounded border border-warn/40 bg-warn/10 px-4 py-3 text-sm">
      <span className="flex items-center gap-2 text-warn">
        <FontAwesomeIcon icon={faTriangleExclamation} aria-hidden /> Hay cambios que requieren reiniciar el servidor.
      </span>
      {canRestart ? (
        <Button onClick={() => setConfirming(true)}>Reiniciar servidor</Button>
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
