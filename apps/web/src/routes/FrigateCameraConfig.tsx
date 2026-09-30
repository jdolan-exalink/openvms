import { faArrowLeft, faArrowsRotate, faCircleCheck, faTriangleExclamation } from "@fortawesome/free-solid-svg-icons";
import { FontAwesomeIcon } from "@fortawesome/react-fontawesome";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { cameraFrigateDocQuery, frigateSchemaQuery, meQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { FrigateHistory } from "@/components/frigate/FrigateHistory";
import { SectionPanel } from "@/components/frigate/SectionPanel";
import { Modal } from "@/components/Modal";
import { Button, ErrorNote, PageHeader } from "@/components/ui";
import { cn } from "@/lib/cn";
import {
  buildPatch, deepEqual, diffValues, formatValue, isLiveSection, type JSchema, kindOf, orderSections, parseVersion, pathLabel, resolve, schemaAt,
  sectionLabel, validateTree,
} from "@/lib/frigateSchema";
import { can } from "@/lib/perm";

type SectionResult = Schemas["FrigateSectionResult"];
const HISTORY = "__history";

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
  const ctx = { root: root ?? {}, secretsVisible, readOnly };
  const header = (
    <PageHeader
      title={`Frigate · ${doc.data?.camera_name ?? "Cámara"}`}
      description={version ? `Configuración de la cámara en Frigate ${version}. Los cambios se aplican al servidor.` : undefined}
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
      {header}
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

      <div className="grid gap-4 md:grid-cols-[14rem_1fr]">
        <nav aria-label="Secciones de configuración" className="flex flex-row gap-1 overflow-x-auto md:flex-col md:overflow-visible">
          {sections.map((s) => (
            <button
              key={s}
              type="button"
              aria-current={activeSection === s ? "page" : undefined}
              onClick={() => setActive(s)}
              className={cn("flex items-center justify-between gap-2 rounded px-3 py-2 text-left text-sm whitespace-nowrap hover:bg-raised", activeSection === s && "bg-raised font-medium")}
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
            className={cn("rounded px-3 py-2 text-left text-sm whitespace-nowrap hover:bg-raised md:mt-2 md:border-t md:border-line", activeSection === HISTORY && "bg-raised font-medium")}
          >
            Historial
          </button>
        </nav>

        <section aria-label={activeSection === HISTORY ? "Historial" : sectionLabel(activeSection ?? "")} className="flex min-w-0 flex-col gap-4 rounded border border-line bg-surface p-4">
          {activeSection === HISTORY ? (
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
            />
            )
          )}
          {activeSection !== HISTORY && Object.entries(errors).filter(([p]) => p.split(".")[0] === activeSection).length > 0 && (
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
                <span className="text-bad line-through">{formatValue(c.before)}</span>
                {" → "}
                <span className="text-ok">{formatValue(c.after)}</span>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
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
