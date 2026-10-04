import { Lock, Plus, RotateCcw, Trash2 } from "lucide-react";
import { Icon } from "../Icon";
import { useId, useState } from "react";
import { TagInput } from "@/components/TagInput";
import { Button, Select, TextInput } from "@/components/ui";
import { deepEqual, humanizeKey, type JSchema, isSecretPath, kindOf, resolve, sectionLabel, validateValue } from "@/lib/frigateSchema";

export type FormCtx = {
  root: JSchema;
  /** Whether the caller may see and edit credentials (servers.config.secrets). */
  secretsVisible: boolean;
  readOnly: boolean;
};

type Path = (string | number)[];
type NodeProps = {
  name: string;
  schema: JSchema | undefined;
  value: unknown;
  onChange: (next: unknown) => void;
  path: Path;
  ctx: FormCtx;
  /** Section roots render without their own label/fieldset chrome. */
  bare?: boolean;
  onRemove?: () => void;
};

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

function emptyFor(schema: JSchema | undefined, root: JSchema): unknown {
  const { node } = resolve(schema, root);
  if (node.default !== undefined) return structuredClone(node.default);
  switch (kindOf(node, root)) {
    case "boolean": return false;
    case "integer":
    case "number": return node.minimum ?? 0;
    case "string": return "";
    case "tags":
    case "array": return [];
    case "object":
    case "map": return {};
    default: return null;
  }
}

function LockedField({ label }: { label: string }) {
  return (
    <div className="flex flex-col gap-1 text-sm">
      <span className="font-medium">{label}</span>
      <span title="Requiere permiso de credenciales" className="inline-flex items-center gap-2 rounded border border-line bg-raised px-3 py-1.5 text-muted">
        <Icon icon={Lock} size="xs" />
        <span aria-hidden>••••••••</span>
        <span className="sr-only">Protegido: requiere permiso de credenciales</span>
      </span>
    </div>
  );
}

function ResetButton({ onClick }: { onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="inline-flex items-center gap-1 text-xs text-muted hover:text-ink focus-visible:outline-2 focus-visible:outline-accent">
      <Icon icon={RotateCcw} size="xs" /> Usar valor por defecto
    </button>
  );
}

function RemoveButton({ name, onRemove }: { name: string; onRemove: () => void }) {
  return (
    <button type="button" aria-label={`Quitar ${name}`} onClick={(e) => { e.preventDefault(); onRemove(); }} className="text-muted hover:text-bad focus-visible:outline-2 focus-visible:outline-accent">
      <Icon icon={Trash2} size="xs" />
    </button>
  );
}

function Help({ text, error }: { text?: string; error?: string }) {
  return (
    <>
      {text && <span className="text-xs text-muted">{text}</span>}
      {error && <span role="alert" className="text-xs text-bad">{error}</span>}
    </>
  );
}

/** JsonField edits shapes without a friendly control (unions, free-form objects) as JSON text. */
function JsonField({ label, value, onChange, disabled }: { label: string; value: unknown; onChange: (v: unknown) => void; disabled: boolean }) {
  const id = useId();
  const [text, setText] = useState(() => JSON.stringify(value ?? null, null, 2));
  const [bad, setBad] = useState(false);
  return (
    <div className="flex flex-col gap-1 text-sm">
      <label htmlFor={id} className="font-medium">{label}</label>
      <textarea
        id={id}
        rows={4}
        disabled={disabled}
        value={text}
        aria-invalid={bad}
        onChange={(e) => {
          setText(e.target.value);
          try {
            onChange(JSON.parse(e.target.value));
            setBad(false);
          } catch {
            setBad(true);
          }
        }}
        className="rounded border border-line bg-bg px-3 py-1.5 font-mono text-xs focus-visible:outline-2 focus-visible:outline-accent"
      />
      {bad && <span role="alert" className="text-xs text-bad">JSON no válido; el cambio no se aplicó.</span>}
    </div>
  );
}

function Collapsible({ title, hint, children, defaultOpen = false, actions }: { title: string; hint?: string; children: React.ReactNode; defaultOpen?: boolean; actions?: React.ReactNode }) {
  return (
    <details open={defaultOpen} className="rounded border border-line bg-surface/50">
      <summary className="flex cursor-pointer flex-wrap items-center justify-between gap-2 px-3 py-2 text-sm font-medium">
        <span>{title}</span>
        {actions}
      </summary>
      <div className="flex flex-col gap-3 border-t border-line p-3">
        {hint && <p className="text-xs text-muted">{hint}</p>}
        {children}
      </div>
    </details>
  );
}

export function FieldNode(props: NodeProps) {
  const { name, schema, value, onChange, path, ctx, bare, onRemove } = props;
  const id = useId();
  const { node, nullable } = resolve(schema, ctx.root);
  const kind = kindOf(node, ctx.root);
  const label = bare ? sectionLabel(name) : node.title || humanizeKey(name);
  const disabled = ctx.readOnly;
  const secret = isSecretPath(path);
  if (secret && !ctx.secretsVisible) return <LockedField label={label} />;

  const placeholder = node.default !== undefined && typeof node.default !== "object" ? String(node.default) : undefined;
  const error = validateValue(node, value, kind);
  const canReset = !disabled && value !== undefined && ((node.default !== undefined && !deepEqual(value, node.default)) || (node.default === undefined && nullable && value !== null));
  const reset = () => onChange(node.default !== undefined ? structuredClone(node.default) : null);
  const resetBtn = canReset && kind !== "object" && kind !== "map" ? <ResetButton onClick={reset} /> : null;

  switch (kind) {
    case "boolean":
      return (
        <div className="flex flex-col gap-1 text-sm">
          <label className="flex items-center gap-2">
            <input id={id} type="checkbox" role="switch" disabled={disabled} checked={value === true || (value === undefined && node.default === true)} onChange={(e) => onChange(e.target.checked)} />
            <span className="font-medium">{label}</span>
            {resetBtn}
          </label>
          <Help text={node.description} />
        </div>
      );
    case "enum":
      return (
        <div className="flex flex-col gap-1 text-sm">
          <label htmlFor={id} className="font-medium">{label}</label>
          <Select id={id} disabled={disabled} value={value === undefined || value === null ? "" : String(value)} onChange={(e) => onChange(node.enum?.find((o) => String(o) === e.target.value) ?? e.target.value)}>
            {(value === undefined || value === null) && <option value="">{placeholder ? `(por defecto: ${placeholder})` : "(por defecto)"}</option>}
            {node.enum?.map((o) => <option key={String(o)} value={String(o)}>{String(o)}</option>)}
          </Select>
          <Help text={node.description} error={error} />
          {resetBtn}
        </div>
      );
    case "integer":
    case "number":
      return (
        <div className="flex flex-col gap-1 text-sm">
          <label htmlFor={id} className="font-medium">{label}</label>
          <TextInput
            id={id}
            type="number"
            disabled={disabled}
            aria-invalid={!!error}
            min={node.minimum}
            max={node.maximum}
            step={kind === "integer" ? 1 : "any"}
            placeholder={placeholder}
            value={typeof value === "number" ? value : ""}
            onChange={(e) => onChange(e.target.value === "" ? (nullable ? null : undefined) : Number(e.target.value))}
          />
          <Help text={node.description} error={error} />
          {resetBtn}
        </div>
      );
    case "string":
      return (
        <div className="flex flex-col gap-1 text-sm">
          <label htmlFor={id} className="font-medium">{label}</label>
          <TextInput id={id} disabled={disabled} placeholder={placeholder} value={typeof value === "string" ? value : ""} onChange={(e) => onChange(e.target.value === "" && nullable ? null : e.target.value)} />
          <Help text={node.description} error={error} />
          {resetBtn}
        </div>
      );
    case "tags": {
      const numeric = ["integer", "number"].includes(String(resolve(node.items, ctx.root).node.type));
      const list = Array.isArray(value) ? value.map(String) : [];
      return disabled ? (
        <div className="flex flex-col gap-1 text-sm">
          <span className="font-medium">{label}</span>
          <span className="text-muted">{list.join(", ") || "—"}</span>
        </div>
      ) : (
        <div className="flex flex-col gap-1">
          <TagInput label={label} hint={node.description} value={list} onChange={(next) => onChange(numeric ? next.map(Number).filter((n) => !Number.isNaN(n)) : next)} />
          <Help error={error} />
          {resetBtn}
        </div>
      );
    }
    case "object": {
      const obj = isObj(value) ? value : {};
      const props = Object.entries(node.properties ?? {}).filter(([k]) => k !== "enabled_in_config");
      const children = props.map(([k, child]) => (
        <FieldNode key={k} name={k} schema={child} value={obj[k]} path={[...path, k]} ctx={ctx} onChange={(v) => onChange({ ...obj, [k]: v })} />
      ));
      if (bare) return <div className="flex flex-col gap-4">{children}</div>;
      return (
        <Collapsible title={label} hint={node.description} actions={onRemove && <RemoveButton name={name} onRemove={onRemove} />}>
          {children}
        </Collapsible>
      );
    }
    case "map": {
      const obj = isObj(value) ? value : {};
      return <MapField {...props} label={label} obj={obj} node={node} bare={bare} onRemove={onRemove} />;
    }
    case "array": {
      const list = Array.isArray(value) ? value : [];
      const body = (
        <div className="flex flex-col gap-3">
          {list.map((item, i) => (
            <FieldNode
              key={i}
              name={`${name} ${i + 1}`}
              schema={node.items}
              value={item}
              path={[...path, i]}
              ctx={ctx}
              onChange={(v) => onChange(list.map((x, j) => (j === i ? v : x)))}
              onRemove={disabled ? undefined : () => onChange(list.filter((_, j) => j !== i))}
            />
          ))}
          {!disabled && (
            <div>
              <Button onClick={() => onChange([...list, emptyFor(node.items, ctx.root)])}>
                <Icon icon={Plus} size="xs" /> Añadir
              </Button>
            </div>
          )}
          <Help error={error} />
        </div>
      );
      return bare ? body : <Collapsible title={label} hint={node.description} defaultOpen={list.length > 0 && list.length < 3}>{body}</Collapsible>;
    }
    default:
      return <JsonField label={label} value={value} disabled={disabled} onChange={onChange} />;
  }
}

function MapField({ name, path, ctx, label, obj, node, bare, onChange, onRemove }: NodeProps & { label: string; obj: Record<string, unknown>; node: JSchema }) {
  const [newKey, setNewKey] = useState("");
  const duplicate = newKey !== "" && newKey in obj;
  const add = () => {
    const k = newKey.trim();
    if (!k || k in obj) return;
    onChange({ ...obj, [k]: emptyFor(node.additionalProperties as JSchema, ctx.root) });
    setNewKey("");
  };
  const body = (
    <div className="flex flex-col gap-3">
      {Object.entries(obj).map(([k, v]) => (
        <FieldNode
          key={k}
          name={k}
          schema={node.additionalProperties as JSchema}
          value={v}
          path={[...path, k]}
          ctx={ctx}
          onChange={(next) => onChange({ ...obj, [k]: next })}
          onRemove={ctx.readOnly ? undefined : () => onChange(Object.fromEntries(Object.entries(obj).filter(([x]) => x !== k)))}
        />
      ))}
      {Object.keys(obj).length === 0 && <p className="text-xs text-muted">Sin entradas.</p>}
      {!ctx.readOnly && (
        <div className="flex flex-wrap items-end gap-2">
          <label className="flex min-w-40 flex-1 flex-col gap-1 text-sm">
            <span className="font-medium">Nueva clave</span>
            <TextInput value={newKey} onChange={(e) => setNewKey(e.target.value)} aria-invalid={duplicate} onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); add(); } }} />
          </label>
          <Button disabled={!newKey.trim() || duplicate} onClick={add}>
            <Icon icon={Plus} size="xs" /> Añadir
          </Button>
        </div>
      )}
      {duplicate && <span role="alert" className="text-xs text-bad">Esa clave ya existe.</span>}
    </div>
  );
  const remove = onRemove && <RemoveButton name={name} onRemove={onRemove} />;
  return bare ? body : <Collapsible title={label} hint={node.description} actions={remove}>{body}</Collapsible>;
}

/** SchemaForm renders one camera section (its schema node is the root of the form). */
export function SchemaForm({ name, schema, value, onChange, ctx }: { name: string; schema: JSchema | undefined; value: unknown; onChange: (v: unknown) => void; ctx: FormCtx }) {
  return <FieldNode name={name} schema={schema} value={value} onChange={onChange} path={[name]} ctx={ctx} bare />;
}
