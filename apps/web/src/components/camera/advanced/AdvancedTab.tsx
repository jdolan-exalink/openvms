import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { Schemas } from "@/api/client";
import { cameraAdvancedQuery } from "@/api/queries";
import { cn } from "@/lib/cn";
import { SyncStatusBadge } from "../SyncStatusBadge";
import { ChangeHistory } from "./ChangeHistory";
import { fmtValue } from "./rollback";
import { YamlSectionEditor } from "./YamlSectionEditor";

type Advanced = Schemas["CameraAdvanced"];
export type Engine = "frigate" | "onvif" | "provision";
const ENGINE_LABEL: Record<Engine, string> = { frigate: "Frigate", onvif: "ONVIF", provision: "Provision" };

function Row({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div className="flex flex-wrap gap-2 text-sm"><dt className="min-w-40 font-medium text-muted">{k}</dt><dd className="break-all">{v}</dd></div>
  );
}

function FrigatePanel({ cameraId, data, yamlSection, onYamlSection, canYaml }: { cameraId: string; data: Advanced["frigate"]; yamlSection: string; onYamlSection: (s: string) => void; canYaml: boolean }) {
  const sections = data.sections.includes(yamlSection) ? data.sections : [yamlSection, ...data.sections];
  return (
    <div className="flex flex-col gap-4">
      <dl aria-label="Configuración generada" className="flex flex-col gap-1">
        <Row k="Versión del motor" v={data.version || "—"} />
        <Row k="Estado" v={<SyncStatusBadge state={data.sync_state as Schemas["CameraConfigState"]["sync_state"]} />} />
        <Row k="Revisión deseada / aplicada" v={`${data.desired_revision} / ${data.applied_revision}`} />
        <Row k="Secciones generadas" v={data.sections.join(", ") || "—"} />
        <Row k="Con parámetros de OpenVMS" v={data.managed_sections.join(", ") || "Ninguna"} />
      </dl>
      <section aria-label="Editar YAML de esta cámara" className="flex flex-col gap-3">
        <h3 className="text-sm font-bold">Editar YAML de esta cámara</h3>
        {canYaml && (
          <label className="flex items-center gap-2 text-sm">
            Sección
            <select aria-label="Sección" className="min-h-9 rounded-m3-lg border border-outline-variant bg-surface-1 px-2" value={yamlSection} onChange={(e) => onYamlSection(e.target.value)}>
              {sections.map((s) => <option key={s} value={s}>{s}</option>)}
            </select>
          </label>
        )}
        <YamlSectionEditor key={yamlSection} cameraId={cameraId} section={yamlSection} canEdit={canYaml} readOnlyReason="Editar YAML requiere el permiso cameras.config.yaml." />
      </section>
    </div>
  );
}

function OnvifPanel({ data }: { data: Advanced["onvif"] }) {
  const caps = Object.entries(data.capabilities);
  const reported = Object.entries(data.reported);
  return (
    <div className="flex flex-col gap-4">
      <dl className="flex flex-col gap-1">
        <Row k="Endpoint" v={data.endpoint || "—"} />
        <Row k="Fabricante / modelo" v={[data.manufacturer, data.model].filter(Boolean).join(" ") || "—"} />
        <Row k="Firmware" v={data.firmware || "—"} />
        <Row k="Perfil activo" v={data.profile || "—"} />
      </dl>
      <section aria-label="Perfiles ONVIF" className="flex flex-col gap-1">
        <h3 className="text-sm font-bold">Perfiles</h3>
        {data.profiles.length === 0 ? <p className="text-sm text-muted">La cámara no reportó perfiles.</p> : (
          <ul className="flex flex-col gap-2">
            {data.profiles.map((p) => (
              <li key={p.token} className="rounded-m3-lg bg-surface-2 p-3 text-sm">
                <p className="font-medium">{p.name} <span className="text-xs text-muted">({p.token})</span></p>
                <p className="text-xs">{p.encoding} · {p.width}x{p.height} · {p.fps} fps · {p.bitrate} kbps</p>
                <p className="break-all text-xs text-muted">{p.stream_uri || "—"}</p>
              </li>
            ))}
          </ul>
        )}
      </section>
      <section aria-label="Capacidades" className="flex flex-col gap-1">
        <h3 className="text-sm font-bold">Capacidades</h3>
        <ul className="flex flex-wrap gap-2 text-xs">
          {caps.map(([k, v]) => <li key={k} className={cn("rounded-m3-lg px-2 py-1", v ? "bg-ok/15 text-ok" : "bg-surface-2 text-muted")}>{k}: {v ? "sí" : "no"}</li>)}
        </ul>
        {data.services.length > 0 && <p className="break-all text-xs text-muted">Servicios: {data.services.join(", ")}</p>}
      </section>
      {reported.length > 0 && (
        <dl aria-label="Parámetros reportados" className="flex flex-col gap-1">
          <h3 className="text-sm font-bold">Parámetros reportados</h3>
          {reported.map(([k, v]) => <Row key={k} k={k} v={fmtValue(v)} />)}
        </dl>
      )}
    </div>
  );
}

function ProvisionPanel({ data }: { data: Advanced["provision"] }) {
  return (
    <dl className="flex flex-col gap-1">
      <Row k="Estado del driver" v={data.link.reachable ? (data.link.online ? "En línea" : data.link.registered ? "Registrada, sin conexión" : "No registrada") : "El agente no respondió"} />
      <Row k="Fabricante / modelo" v={[data.manufacturer, data.model].filter(Boolean).join(" ") || "—"} />
      <Row k="Eventos recibidos" v={data.link.event_count} />
      <Row k="Último evento" v={data.link.last_event_at ? new Date(data.link.last_event_at).toLocaleString() : "—"} />
      <Row k="Etiquetas" v={data.tags.join(", ") || "—"} />
    </dl>
  );
}

/**
 * Avanzado: technical detail separated by backend. Frigate (generated config, contextual YAML), ONVIF
 * and Provision are read-only except for the YAML editor; only the engines that have data get a panel.
 */
export function AdvancedTab({ cameraId, engine, yamlRequest, canAdvanced, canYaml, canRollback }: { cameraId: string; engine?: Engine; yamlRequest?: { section: string }; canAdvanced: boolean; canYaml: boolean; canRollback: boolean }) {
  const q = useQuery({ ...cameraAdvancedQuery(cameraId), enabled: canAdvanced });
  const [picked, setPicked] = useState<Engine>();
  const [section, setSection] = useState(yamlRequest?.section ?? "detect");
  // A contextual entry point ("Editar YAML de detección") preselects the section and the Frigate panel.
  const [seen, setSeen] = useState(yamlRequest);
  if (yamlRequest !== seen) {
    setSeen(yamlRequest);
    if (yamlRequest) { setSection(yamlRequest.section); setPicked("frigate"); }
  }

  const d = q.data;
  const present: Engine[] = d ? [...(d.frigate.available ? ["frigate" as const] : []), ...(d.onvif.configured ? ["onvif" as const] : []), ...(d.provision.evidence ? ["provision" as const] : [])] : [];
  const active = picked && present.includes(picked) ? picked : engine && present.includes(engine) ? engine : present[0];

  return (
    <div className="flex flex-col gap-5">
      {!canAdvanced ? (
        <p role="status" className="rounded-m3-lg bg-surface-2 px-4 py-3 text-sm text-muted">Los datos técnicos requieren el permiso cameras.config.advanced.</p>
      ) : q.isLoading ? (
        <p role="status" className="text-sm text-muted">Cargando datos técnicos…</p>
      ) : q.isError || !d ? (
        <p role="alert" className="text-sm text-bad">No se pudieron cargar los datos técnicos.</p>
      ) : present.length === 0 ? (
        <p className="text-sm text-muted">No hay datos técnicos disponibles para esta cámara.{d.frigate.reason ? ` ${d.frigate.reason}` : ""}</p>
      ) : (
        <>
          <nav aria-label="Motor" className="flex flex-wrap gap-2">
            {present.map((e) => (
              <button key={e} type="button" aria-pressed={active === e} onClick={() => setPicked(e)}
                className={cn("inline-flex min-h-9 items-center rounded-m3-lg px-3 text-xs font-bold transition-colors", active === e ? "bg-primary text-on-primary" : "bg-surface-2 text-on-surface hover:bg-surface-3")}>
                {ENGINE_LABEL[e]}
              </button>
            ))}
          </nav>
          <section aria-label={`Panel ${active ? ENGINE_LABEL[active] : ""}`}>
            {active === "frigate" && <FrigatePanel cameraId={cameraId} data={d.frigate} yamlSection={section} onYamlSection={setSection} canYaml={canYaml} />}
            {active === "onvif" && <OnvifPanel data={d.onvif} />}
            {active === "provision" && <ProvisionPanel data={d.provision} />}
          </section>
        </>
      )}
      <ChangeHistory cameraId={cameraId} canRollback={canRollback} />
    </div>
  );
}
