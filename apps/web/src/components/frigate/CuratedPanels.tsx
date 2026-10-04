import { Lock, Plus, Trash2 } from "lucide-react";
import { Icon } from "../Icon";
import type { ReactNode } from "react";
import { LabelPicker } from "@/components/frigate/LabelPicker";
import type { FormCtx } from "@/components/frigate/SchemaForm";
import { Button, Select, TextInput } from "@/components/ui";
import { getIn, type JSchema, resolve, schemaAt, setIn } from "@/lib/frigateSchema";
import { labelDisplay } from "@/lib/labelEmoji";

type Path = (string | number)[];

/** Section editor used by the curated panels: typed access to paths plus schema metadata. */
type Ed = {
  get: (p: Path) => unknown;
  set: (p: Path, v: unknown) => void;
  /** Schema node at a path, or undefined when this Frigate version does not know the field. */
  meta: (p: Path) => JSchema | undefined;
  ctx: FormCtx;
};

function makeEd(schema: JSchema | undefined, value: unknown, onChange: (v: unknown) => void, ctx: FormCtx): Ed {
  return {
    get: (p) => getIn(value, p),
    set: (p, v) => onChange(setIn(value, p, v)),
    meta: (p) => (schema ? schemaAt(ctx.root, schema, p) : undefined),
    ctx,
  };
}

/** A field is shown only when the schema or the effective config knows it (versions differ). */
const known = (ed: Ed, p: Path) => ed.meta(p) !== undefined || ed.get(p) !== undefined;

function Row({ children }: { children: ReactNode }) {
  return <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{children}</div>;
}

function Switch({ ed, path, label, hint }: { ed: Ed; path: Path; label: string; hint?: string }) {
  if (!known(ed, path)) return null;
  const def = ed.meta(path)?.default;
  return (
    <div className="flex flex-col gap-1 text-sm">
      <label className="flex items-center gap-2">
        <input type="checkbox" role="switch" disabled={ed.ctx.readOnly} checked={(ed.get(path) ?? def) === true} onChange={(e) => ed.set(path, e.target.checked)} />
        <span className="font-medium">{label}</span>
      </label>
      {hint && <span className="text-xs text-muted">{hint}</span>}
    </div>
  );
}

function Num({ ed, path, label, hint, unit }: { ed: Ed; path: Path; label: string; hint?: string; unit?: string }) {
  if (!known(ed, path)) return null;
  const node = ed.meta(path);
  const v = ed.get(path);
  const placeholder = node?.default !== undefined ? String(node.default) : undefined;
  const bad = typeof v === "number" && ((node?.minimum !== undefined && v < node.minimum) || (node?.maximum !== undefined && v > node.maximum));
  return (
    <label className="flex flex-col gap-1 text-sm">
      <span className="font-medium">{label}{unit && <span className="font-normal text-muted"> ({unit})</span>}</span>
      <TextInput
        type="number"
        disabled={ed.ctx.readOnly}
        min={node?.minimum}
        max={node?.maximum}
        step={node?.type === "integer" ? 1 : "any"}
        placeholder={placeholder}
        aria-invalid={bad}
        value={typeof v === "number" ? v : ""}
        onChange={(e) => ed.set(path, e.target.value === "" ? undefined : Number(e.target.value))}
      />
      {bad && <span role="alert" className="text-xs text-bad">Fuera de rango{node?.minimum !== undefined ? ` (mín. ${node.minimum}` : ""}{node?.maximum !== undefined ? `${node?.minimum !== undefined ? ", " : " ("}máx. ${node.maximum}` : ""}{node?.minimum !== undefined || node?.maximum !== undefined ? ")" : ""}.</span>}
      {hint && <span className="text-xs text-muted">{hint}</span>}
    </label>
  );
}

function Pick({ ed, path, label, options }: { ed: Ed; path: Path; label: string; options?: { value: string; label: string }[] }) {
  if (!known(ed, path)) return null;
  const node = ed.meta(path);
  const opts = options ?? (node?.enum ?? []).map((o) => ({ value: String(o), label: String(o) }));
  if (opts.length === 0) return null;
  const v = ed.get(path);
  const cur = typeof v === "string" ? v : "";
  return (
    <label className="flex flex-col gap-1 text-sm">
      <span className="font-medium">{label}</span>
      <Select disabled={ed.ctx.readOnly} value={cur} onChange={(e) => ed.set(path, e.target.value)}>
        {(cur === "" || !opts.some((o) => o.value === cur)) && <option value={cur}>{cur === "" ? "(por defecto)" : `${cur} (actual)`}</option>}
        {opts.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
      </Select>
    </label>
  );
}

const retainModes = [
  { value: "all", label: "Todo" },
  { value: "motion", label: "Solo con movimiento" },
  { value: "active_objects", label: "Solo con objetos activos" },
];

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-3">
      <h3 className="text-sm font-semibold">{title}</h3>
      {children}
    </div>
  );
}

// --- sections ---------------------------------------------------------------------------------

const HWACCEL = [
  "auto", "preset-vaapi", "preset-intel-qsv-h264", "preset-intel-qsv-h265", "preset-nvidia", "preset-jetson-h264", "preset-jetson-h265",
  "preset-rk-h264", "preset-rk-h265", "preset-rpi-64-h264", "preset-rpi-64-h265",
].map((v) => ({ value: v, label: v === "auto" ? "Automático" : v }));
const ROLES = [{ id: "detect", label: "Detección" }, { id: "record", label: "Grabación" }, { id: "audio", label: "Audio" }];

function Ffmpeg({ ed }: { ed: Ed }) {
  const inputs = (Array.isArray(ed.get(["inputs"])) ? ed.get(["inputs"]) : []) as { path?: string; roles?: string[] }[];
  const locked = !ed.ctx.secretsVisible;
  const editable = !ed.ctx.readOnly && !locked;
  const setInputs = (next: unknown[]) => ed.set(["inputs"], next);
  const hwRaw = ed.get(["hwaccel_args"]);
  return (
    <div className="flex flex-col gap-4">
      <Group title="Streams de entrada">
        {locked && (
          <p className="flex items-center gap-2 text-xs text-muted" title="Requiere permiso de credenciales">
            <Icon icon={Lock} size="xs" /> Las rutas y los roles de los streams requieren permiso de credenciales.
          </p>
        )}
        {inputs.map((inp, i) => (
          <div key={i} className="flex flex-col gap-2 rounded border border-line p-3">
            <div className="flex items-end gap-2">
              <label className="flex flex-1 flex-col gap-1 text-sm">
                <span className="font-medium">Ruta del stream {i + 1}</span>
                {locked ? (
                  <span title="Requiere permiso de credenciales" className="inline-flex items-center gap-2 rounded border border-line bg-raised px-3 py-1.5 text-muted">
                    <Icon icon={Lock} size="xs" /> ••••••••
                  </span>
                ) : (
                  <TextInput disabled={!editable} value={inp.path ?? ""} onChange={(e) => setInputs(inputs.map((x, j) => (j === i ? { ...x, path: e.target.value } : x)))} />
                )}
              </label>
              {editable && inputs.length > 1 && (
                <Button aria-label={`Quitar stream ${i + 1}`} onClick={() => setInputs(inputs.filter((_, j) => j !== i))}>
                  <Icon icon={Trash2} size="xs" />
                </Button>
              )}
            </div>
            <fieldset className="flex flex-wrap gap-4 text-sm">
              <legend className="sr-only">Roles del stream {i + 1}</legend>
              {ROLES.map((r) => (
                <label key={r.id} className="flex items-center gap-1.5">
                  <input
                    type="checkbox"
                    disabled={!editable}
                    checked={inp.roles?.includes(r.id) ?? false}
                    onChange={(e) => {
                      const roles = e.target.checked ? [...(inp.roles ?? []), r.id] : (inp.roles ?? []).filter((x) => x !== r.id);
                      setInputs(inputs.map((x, j) => (j === i ? { ...x, roles } : x)));
                    }}
                  />
                  {r.label}
                </label>
              ))}
            </fieldset>
          </div>
        ))}
        {editable && (
          <div>
            <Button onClick={() => setInputs([...inputs, { path: "", roles: ["detect"] }])}>
              <Icon icon={Plus} size="xs" /> Añadir stream
            </Button>
          </div>
        )}
      </Group>
      {Array.isArray(hwRaw) ? (
        <p className="text-xs text-muted">La aceleración por hardware usa argumentos personalizados (se conservan; edítalos en Opciones avanzadas).</p>
      ) : (
        <Pick ed={ed} path={["hwaccel_args"]} label="Aceleración por hardware" options={HWACCEL} />
      )}
    </div>
  );
}

function Detect({ ed }: { ed: Ed }) {
  return (
    <Group title="Detección de objetos">
      <Switch ed={ed} path={["enabled"]} label="Detección habilitada" />
      <Row>
        <Num ed={ed} path={["width"]} label="Ancho" unit="px" />
        <Num ed={ed} path={["height"]} label="Alto" unit="px" />
        <Num ed={ed} path={["fps"]} label="Cuadros por segundo" unit="fps" />
      </Row>
      <p className="text-xs text-muted">Usa la resolución del sub-stream; menos fps y menor resolución reducen la carga del detector, pero pueden perder objetos rápidos.</p>
    </Group>
  );
}

const RETAIN_GROUPS: { title: string; base: Path }[] = [
  { title: "Alertas", base: ["alerts"] },
  { title: "Detecciones", base: ["detections"] },
];

function Record({ ed }: { ed: Ed }) {
  return (
    <div className="flex flex-col gap-5">
      <Group title="Grabación">
        <Switch ed={ed} path={["enabled"]} label="Grabación habilitada" />
        <Row>
          <Num ed={ed} path={["retain", "days"]} label="Retención general" unit="días" />
          <Pick ed={ed} path={["retain", "mode"]} label="Modo de retención" options={retainModes} />
          <Num ed={ed} path={["continuous", "days"]} label="Retención continua" unit="días" />
          <Num ed={ed} path={["motion", "days"]} label="Retención por movimiento" unit="días" />
        </Row>
      </Group>
      {RETAIN_GROUPS.map((g) => (
        <Group key={g.title} title={g.title}>
          <Row>
            <Num ed={ed} path={[...g.base, "retain", "days"]} label="Retención" unit="días" />
            <Pick ed={ed} path={[...g.base, "retain", "mode"]} label="Modo" options={retainModes} />
            <Num ed={ed} path={[...g.base, "pre_capture"]} label="Pre-captura" unit="s" />
            <Num ed={ed} path={[...g.base, "post_capture"]} label="Post-captura" unit="s" />
          </Row>
        </Group>
      ))}
    </div>
  );
}

function Snapshots({ ed }: { ed: Ed }) {
  return (
    <Group title="Capturas">
      <Switch ed={ed} path={["enabled"]} label="Capturas habilitadas" />
      <Row>
        <Switch ed={ed} path={["bounding_box"]} label="Dibujar recuadro" />
        <Switch ed={ed} path={["crop"]} label="Recortar al objeto" />
        <Num ed={ed} path={["height"]} label="Alto" unit="px" />
        <Num ed={ed} path={["retain", "default"]} label="Retención" unit="días" />
        <Num ed={ed} path={["quality"]} label="Calidad JPEG" hint="0 a 100" />
      </Row>
    </Group>
  );
}

const FILTER_FIELDS: { key: string; label: string }[] = [
  { key: "min_area", label: "Área mínima" },
  { key: "max_area", label: "Área máxima" },
  { key: "threshold", label: "Umbral" },
  { key: "min_score", label: "Puntaje mínimo" },
];

function Objects({ ed }: { ed: Ed }) {
  const track = (Array.isArray(ed.get(["track"])) ? ed.get(["track"]) : []) as string[];
  const filters = (ed.get(["filters"]) ?? {}) as Record<string, Record<string, unknown>>;
  return (
    <div className="flex flex-col gap-5">
      <LabelPicker
        label="Objetos a detectar"
        hint="Marca los objetos que Frigate debe rastrear en esta cámara."
        value={track}
        extraOptions={Object.keys(filters)}
        disabled={ed.ctx.readOnly}
        onChange={(next) => ed.set(["track"], next)}
      />
      {track.length > 0 && (
        <Group title="Filtros por objeto">
          {track.map((l) => (
            <details key={l} className="rounded border border-line">
              <summary className="cursor-pointer px-3 py-2 text-sm font-medium">{labelDisplay(l)}</summary>
              <div className="border-t border-line p-3">
                <Row>
                  {FILTER_FIELDS.map((f) => {
                    const p: Path = ["filters", l, f.key];
                    const node = ed.meta(["filters", l, f.key]);
                    const v = ed.get(p);
                    return (
                      <label key={f.key} className="flex flex-col gap-1 text-sm">
                        <span className="font-medium">{f.label}</span>
                        <TextInput
                          type="number"
                          disabled={ed.ctx.readOnly}
                          step="any"
                          min={node?.minimum}
                          max={node?.maximum}
                          value={typeof v === "number" ? v : ""}
                          onChange={(e) => ed.set(p, e.target.value === "" ? undefined : Number(e.target.value))}
                        />
                      </label>
                    );
                  })}
                </Row>
              </div>
            </details>
          ))}
        </Group>
      )}
    </div>
  );
}

function Motion({ ed }: { ed: Ed }) {
  return (
    <Group title="Detección de movimiento">
      <Row>
        <Num ed={ed} path={["threshold"]} label="Umbral" hint="Más alto = menos sensible." />
        <Num ed={ed} path={["contour_area"]} label="Área mínima de contorno" />
      </Row>
      <Switch ed={ed} path={["improve_contrast"]} label="Mejorar contraste" />
    </Group>
  );
}

function ZoneChecks({ label, zones, value, onChange, disabled }: { label: string; zones: string[]; value: string[]; onChange: (v: string[]) => void; disabled: boolean }) {
  return (
    <fieldset className="flex flex-col gap-1 text-sm">
      <legend className="font-medium">{label}</legend>
      {zones.length === 0 && <span className="text-xs text-muted">La cámara no tiene zonas.</span>}
      <div className="flex flex-wrap gap-4">
        {zones.map((z) => (
          <label key={z} className="flex items-center gap-1.5">
            <input type="checkbox" disabled={disabled} checked={value.includes(z)} onChange={(e) => onChange(e.target.checked ? [...value, z] : value.filter((x) => x !== z))} />
            {z}
          </label>
        ))}
      </div>
    </fieldset>
  );
}

function Review({ ed, zones }: { ed: Ed; zones: string[] }) {
  const list = (p: Path) => (Array.isArray(ed.get(p)) ? (ed.get(p) as string[]) : []);
  return (
    <div className="flex flex-col gap-5">
      {RETAIN_GROUPS.map((g) => (
        <Group key={g.title} title={g.title}>
          <LabelPicker label={`Objetos que generan ${g.title.toLowerCase()}`} value={list([...g.base, "labels"])} disabled={ed.ctx.readOnly} onChange={(v) => ed.set([...g.base, "labels"], v)} />
          <ZoneChecks label="Zonas requeridas" zones={zones} value={list([...g.base, "required_zones"])} disabled={ed.ctx.readOnly} onChange={(v) => ed.set([...g.base, "required_zones"], v)} />
        </Group>
      ))}
    </div>
  );
}

function Zones({ onEditZones, readOnly }: { onEditZones?: () => void; readOnly: boolean }) {
  return (
    <Group title="Zonas y máscaras">
      <div>
        <Button disabled={!onEditZones || readOnly} title={readOnly ? "Solo lectura: requiere el permiso servers.config y Frigate 0.16 o superior" : undefined} onClick={onEditZones}>
          Editar zonas
        </Button>
      </div>
    </Group>
  );
}

/** hasCurated lists the sections that get a friendlier panel above the generic form. */
export const CURATED = new Set(["ffmpeg", "detect", "record", "snapshots", "objects", "motion", "review", "zones"]);

export function CuratedPanel({
  section, schema, value, onChange, ctx, zones, onEditZones,
}: {
  section: string;
  schema: JSchema | undefined;
  value: unknown;
  onChange: (v: unknown) => void;
  ctx: FormCtx;
  zones: string[];
  onEditZones?: () => void;
}) {
  const node = schema ? resolve(schema, ctx.root).node : undefined;
  const ed = makeEd(node, value, onChange, ctx);
  switch (section) {
    case "ffmpeg": return <Ffmpeg ed={ed} />;
    case "detect": return <Detect ed={ed} />;
    case "record": return <Record ed={ed} />;
    case "snapshots": return <Snapshots ed={ed} />;
    case "objects": return <Objects ed={ed} />;
    case "motion": return <Motion ed={ed} />;
    case "review": return <Review ed={ed} zones={zones} />;
    case "zones": return <Zones onEditZones={onEditZones} readOnly={ctx.readOnly} />;
    default: return null;
  }
}
