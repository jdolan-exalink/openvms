import { HelpCircle, RefreshCw, X } from "lucide-react";
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Button, Select } from "@/components/ui";
import { knownLabels, labelName } from "@/lib/format";
import { useFocusTrap } from "@/lib/useFocusTrap";
import { type Point, hasSelfIntersection, isValidZoneName, newId, removeVertex } from "@/lib/zoneGeometry";
import { type DrawingState, ZoneCanvas } from "./ZoneCanvas";
import {
  type Draft,
  type EditorItem,
  ITEM_COLORS,
  type ItemIssues,
  type ItemKind,
  type ZoneEditorValue,
  buildDraft,
  draftToValue,
  isDictFormat,
  maskFormatOf,
} from "./zoneDraft";
import { TAB_LABELS, ZoneList } from "./ZoneList";
import { type LabelPickerProps, ZoneProperties } from "./ZoneProperties";

export type { ZoneEditorValue } from "./zoneDraft";
export type { LabelPickerProps } from "./ZoneProperties";

export interface ZoneEditorModalProps {
  open: boolean;
  onClose: () => void;
  cameraId: string;
  /** Camera frame URL, e.g. `/media/v1/cameras/{id}/snapshot.jpg`. */
  snapshotUrl: string;
  /** Detect resolution; used to convert legacy absolute coordinates. Falls back to the snapshot size. */
  frameWidth?: number;
  frameHeight?: number;
  value: ZoneEditorValue;
  /** Frigate version string (e.g. "0.18.0-abc"); decides the native mask format for empty fields. */
  frigateVersion: string | null | undefined;
  /** Receives the edited value with mask formats preserved. The parent persists it and closes the modal. */
  onSave: (next: ZoneEditorValue) => void;
  /** Optional label picker (e.g. the emoji picker); a checkbox group is used when omitted. */
  renderLabelPicker?: (props: LabelPickerProps) => ReactNode;
  /** Labels offered by the fallback picker and for object-mask scopes. Defaults to the known labels. */
  availableLabels?: string[];
}

const SHORTCUTS: [string, string][] = [
  ["Clic", "Añadir punto (al dibujar) o seleccionar una figura"],
  ["Clic en el primer punto / Enter", "Cerrar el polígono (mínimo 3 puntos)"],
  ["Retroceso (dibujando)", "Quitar el último punto"],
  ["Esc", "Cancelar el dibujo; fuera del dibujo, cerrar el editor"],
  ["Arrastrar un punto", "Mover el vértice"],
  ["Clic en un borde", "Insertar un vértice"],
  ["Clic derecho / Supr sobre un vértice", "Eliminar el vértice (mínimo 3)"],
  ["Arrastrar el interior", "Mover toda la figura"],
];

export function ZoneEditorModal(props: ZoneEditorModalProps) {
  if (!props.open) return null;
  return <ZoneEditorDialog {...props} />;
}

const isTextTarget = (t: EventTarget | null) => t instanceof HTMLElement && /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName);

function validate(items: EditorItem[]): Record<string, ItemIssues> {
  const out: Record<string, ItemIssues> = {};
  const counts = new Map<string, number>();
  items.filter((i) => i.kind === "zone").forEach((i) => counts.set(i.name, (counts.get(i.name) ?? 0) + 1));
  for (const it of items) {
    const errors: string[] = [];
    const warnings: string[] = [];
    if (it.points.length < 3) errors.push("Se necesitan al menos 3 puntos.");
    if (it.kind === "zone") {
      if (!isValidZoneName(it.name)) errors.push("El nombre solo admite minúsculas, números y guion bajo.");
      else if ((counts.get(it.name) ?? 0) > 1) errors.push("Ya existe una zona con ese nombre.");
    }
    if (hasSelfIntersection(it.points)) warnings.push("El polígono se cruza consigo mismo; Frigate puede interpretarlo de forma inesperada.");
    out[it.uid] = { errors, warnings };
  }
  return out;
}

function uniqueZoneName(items: EditorItem[]): string {
  const taken = new Set(items.filter((i) => i.kind === "zone").map((i) => i.name));
  for (let n = 1; ; n++) if (!taken.has(`zona_${n}`)) return `zona_${n}`;
}

type Confirm = { type: "delete"; uid: string } | { type: "discard" } | null;

function ZoneEditorDialog({
  onClose,
  cameraId,
  snapshotUrl,
  frameWidth,
  frameHeight,
  value,
  frigateVersion,
  onSave,
  renderLabelPicker,
  availableLabels,
}: ZoneEditorModalProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [natural, setNatural] = useState<{ w: number; h: number } | null>(null);
  const [tab, setTab] = useState<ItemKind>("zone");
  const [selectedUid, setSelectedUid] = useState<string | null>(null);
  const [selectedVertex, setSelectedVertex] = useState<number | null>(null);
  const [hidden, setHidden] = useState<Set<string>>(new Set());
  const [drawing, setDrawing] = useState<(DrawingState & { kind: ItemKind; scope: string }) | null>(null);
  const [objectScope, setObjectScope] = useState("");
  const [confirm, setConfirm] = useState<Confirm>(null);
  const [dirty, setDirty] = useState(false);
  const [showHelp, setShowHelp] = useState(false);
  const [nonce, setNonce] = useState(() => Date.now());

  const labels = availableLabels ?? knownLabels;
  const frameKnown = (!!frameWidth && !!frameHeight) || natural !== null;

  // Build the draft once the frame size is known, so legacy absolute coordinates convert correctly.
  if (!draft && frameKnown) {
    const frame =
      frameWidth && frameHeight ? { width: frameWidth, height: frameHeight } : natural && natural.w ? { width: natural.w, height: natural.h } : undefined;
    setDraft(buildDraft(value, frame, frigateVersion));
  }

  const items = useMemo(() => draft?.items ?? [], [draft]);
  const issues = useMemo(() => validate(items), [items]);
  const hasErrors = Object.values(issues).some((i) => i.errors.length > 0);
  const tabItems = items.filter((i) => i.kind === tab);
  const selected = items.find((i) => i.uid === selectedUid) ?? null;

  const live = useRef({ drawing, confirm, dirty });
  useEffect(() => {
    live.current = { drawing, confirm, dirty };
  });

  function requestClose() {
    if (live.current.dirty) setConfirm({ type: "discard" });
    else onClose();
  }

  useFocusTrap(containerRef, () => {
    const { drawing: d, confirm: c } = live.current;
    if (c) return; // the confirmation dialog handles its own Escape
    if (d) setDrawing(null);
    else requestClose();
  });

  function mutate(fn: (d: Draft) => Draft) {
    setDraft((d) => (d ? fn(d) : d));
    setDirty(true);
  }

  const setPoints = (uid: string, points: Point[]) =>
    mutate((d) => ({ ...d, items: d.items.map((i) => (i.uid === uid ? { ...i, points } : i)) }));

  const patchItem = (uid: string, patch: Partial<EditorItem>) =>
    mutate((d) => ({ ...d, items: d.items.map((i) => (i.uid === uid ? { ...i, ...patch } : i)) }));

  function setConfig(uid: string, key: string, v: unknown) {
    mutate((d) => ({
      ...d,
      items: d.items.map((i) => {
        if (i.uid !== uid) return i;
        const config = { ...i.config };
        if (v === undefined) delete config[key];
        else config[key] = v;
        return { ...i, config };
      }),
    }));
  }

  function startDraw() {
    setSelectedUid(null);
    setSelectedVertex(null);
    const nextColor = draft?.nextColor ?? 0;
    setDrawing({ kind: tab, scope: tab === "object" ? objectScope : "", color: ITEM_COLORS[nextColor % ITEM_COLORS.length]!, points: [] });
  }

  function closeDrawing() {
    if (!drawing || !draft || drawing.points.length < 3) return;
    const uid = newId("item");
    const maskCount = items.filter((i) => i.kind === drawing.kind && i.scope === drawing.scope).length;
    const item: EditorItem = {
      uid,
      kind: drawing.kind,
      scope: drawing.scope,
      name: drawing.kind === "zone" ? uniqueZoneName(items) : `Máscara ${maskCount + 1}`,
      points: drawing.points,
      enabled: true,
      config: drawing.kind === "zone" ? { inertia: 3, loitering_time: 0 } : undefined,
      colorIndex: draft.nextColor,
    };
    mutate((d) => ({ ...d, items: [...d.items, item], nextColor: d.nextColor + 1 }));
    setDrawing(null);
    setSelectedUid(uid);
  }

  function removeVertexAt(i: number) {
    if (!selected) return;
    setPoints(selected.uid, removeVertex(selected.points, i));
    setSelectedVertex(null);
  }

  function deleteItem(uid: string) {
    mutate((d) => ({ ...d, items: d.items.filter((i) => i.uid !== uid) }));
    if (selectedUid === uid) setSelectedUid(null);
    setConfirm(null);
  }

  // Drawing/vertex shortcuts. Text fields keep their native Enter/Backspace/Delete behaviour.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (isTextTarget(e.target) || live.current.confirm) return;
      if (drawing) {
        if (e.key === "Enter") {
          e.preventDefault();
          closeDrawing();
        } else if (e.key === "Backspace") {
          e.preventDefault();
          setDrawing((d) => (d ? { ...d, points: d.points.slice(0, -1) } : d));
        }
      } else if ((e.key === "Delete" || e.key === "Backspace") && selected && selectedVertex !== null) {
        e.preventDefault();
        removeVertexAt(selectedVertex);
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  });

  const scopes = useMemo(() => {
    const fromItems = items.filter((i) => i.kind === "object" && i.scope).map((i) => i.scope);
    return [...new Set([...fromItems, ...labels])];
  }, [items, labels]);

  const aspect = frameWidth && frameHeight ? frameWidth / frameHeight : natural && natural.w ? natural.w / natural.h : 16 / 9;
  const src = `${snapshotUrl}${snapshotUrl.includes("?") ? "&" : "?"}_=${nonce}`;
  const pendingDelete = confirm?.type === "delete" ? items.find((i) => i.uid === confirm.uid) : undefined;

  return (
    <>
      <div className="fixed inset-0 z-50 bg-black/70 p-2 sm:p-4">
        <div
          ref={containerRef}
          role="dialog"
          aria-modal="true"
          aria-label={`Editor de zonas y máscaras de ${cameraId}`}
          className="mx-auto flex size-full max-w-[1800px] flex-col gap-3 overflow-hidden rounded border border-line bg-surface p-3 shadow-lg"
        >
          <div className="relative flex items-center justify-between gap-3">
            <h2 className="truncate text-lg font-semibold">Zonas y máscaras · {cameraId}</h2>
            <div className="flex items-center gap-1">
              <button
                type="button"
                aria-label="Ayuda de atajos"
                aria-expanded={showHelp}
                aria-controls="zone-help"
                onClick={() => setShowHelp((v) => !v)}
                className="rounded p-1 hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent"
              >
                <HelpCircle className="size-5" aria-hidden />
              </button>
              <button type="button" aria-label="Cerrar" onClick={requestClose} className="rounded p-1 hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent">
                <X className="size-5" aria-hidden />
              </button>
            </div>
            {showHelp && (
              <div id="zone-help" role="tooltip" className="absolute top-full right-0 z-10 mt-1 w-96 max-w-full rounded border border-line bg-bg p-3 text-xs shadow-lg">
                <p className="mb-1 font-medium">Atajos</p>
                <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
                  {SHORTCUTS.map(([k, d]) => (
                    <div key={k} className="contents">
                      <dt className="font-mono text-muted">{k}</dt>
                      <dd>{d}</dd>
                    </div>
                  ))}
                </dl>
              </div>
            )}
          </div>

          <div className="grid min-h-0 flex-1 gap-3 lg:grid-cols-[22rem_1fr]">
            <div className="flex min-h-0 flex-col gap-3 overflow-auto pr-1">
              <ZoneList
                tab={tab}
                onTab={(t) => {
                  setTab(t);
                  setSelectedUid(null);
                }}
                items={tabItems}
                selectedUid={selectedUid}
                hidden={hidden}
                issues={issues}
                disabled={!!drawing || !draft}
                onSelect={(uid) => {
                  setSelectedUid(uid);
                  setSelectedVertex(null);
                }}
                onToggleHidden={(uid) =>
                  setHidden((h) => {
                    const n = new Set(h);
                    if (!n.delete(uid)) n.add(uid);
                    return n;
                  })
                }
                onDelete={(uid) => setConfirm({ type: "delete", uid })}
                onNew={startDraw}
              />
              {tab === "object" && (
                <label className="flex flex-col gap-1 text-sm">
                  Ámbito de la nueva máscara
                  <Select value={objectScope} disabled={!!drawing} onChange={(e) => setObjectScope(e.target.value)}>
                    <option value="">Todos los objetos (global)</option>
                    {scopes.map((l) => (
                      <option key={l} value={l}>
                        {labelName(l)}
                      </option>
                    ))}
                  </Select>
                </label>
              )}
              {selected && draft && selected.kind === tab && (
                <ZoneProperties
                  item={selected}
                  issues={issues[selected.uid] ?? { errors: [], warnings: [] }}
                  dictFormat={isDictFormat(maskFormatOf(draft, selected))}
                  labels={labels}
                  renderLabelPicker={renderLabelPicker}
                  onPatch={(p) => patchItem(selected.uid, p)}
                  onConfig={(k, v) => setConfig(selected.uid, k, v)}
                />
              )}
            </div>

            <div className="flex min-h-0 flex-col gap-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p role="status" className="text-sm text-muted">
                  {drawing
                    ? `Dibujando ${TAB_LABELS[drawing.kind].toLowerCase()}: ${drawing.points.length} puntos. Enter para cerrar, Esc para cancelar.`
                    : "Selecciona una figura para editarla o crea una nueva."}
                </p>
                <div className="flex gap-2">
                  {drawing && drawing.points.length >= 3 && <Button onClick={closeDrawing}>Cerrar polígono</Button>}
                  {drawing && <Button onClick={() => setDrawing(null)}>Cancelar dibujo</Button>}
                  <Button aria-label="Actualizar imagen de la cámara" onClick={() => setNonce(Date.now())}>
                    <RefreshCw className="size-4" aria-hidden /> Actualizar imagen
                  </Button>
                </div>
              </div>
              <div className="min-h-64 flex-1 overflow-hidden rounded border border-line">
                <ZoneCanvas
                  items={items}
                  selectedUid={selectedUid}
                  hidden={hidden}
                  drawing={drawing}
                  selectedVertex={selectedVertex}
                  snapshotSrc={src}
                  fallbackAspect={aspect}
                  onNaturalSize={(w, h) => setNatural((n) => n ?? { w, h })}
                  onChangePoints={setPoints}
                  onSelect={(uid) => {
                    setSelectedUid(uid);
                    const k = items.find((i) => i.uid === uid)?.kind;
                    if (k) setTab(k);
                  }}
                  onSelectVertex={setSelectedVertex}
                  onRemoveVertex={removeVertexAt}
                  onDrawPoint={(p) => setDrawing((d) => (d ? { ...d, points: [...d.points, p] } : d))}
                  onDrawClose={closeDrawing}
                />
              </div>
            </div>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-2 border-t border-line pt-3">
            <p role={hasErrors ? "alert" : undefined} className={hasErrors ? "text-sm text-bad" : "text-sm text-muted"}>
              {hasErrors ? "Hay figuras con errores; corrígelas para guardar." : dirty ? "Cambios sin guardar." : "Sin cambios."}
            </p>
            <div className="flex gap-2">
              <Button onClick={requestClose}>Cancelar</Button>
              <Button variant="primary" disabled={!draft || !dirty || hasErrors || !!drawing} onClick={() => draft && onSave(draftToValue(draft, frigateVersion))}>
                Guardar cambios
              </Button>
            </div>
          </div>
        </div>
      </div>
      {confirm?.type === "delete" && pendingDelete && (
        <ConfirmDialog
          title={`Eliminar ${pendingDelete.name}`}
          message={`Se eliminará «${pendingDelete.name}» al guardar los cambios.`}
          confirmLabel="Eliminar"
          onConfirm={() => deleteItem(pendingDelete.uid)}
          onCancel={() => setConfirm(null)}
        />
      )}
      {confirm?.type === "discard" && (
        <ConfirmDialog
          title="Descartar cambios"
          message="Hay cambios sin guardar en las zonas y máscaras. ¿Descartarlos?"
          confirmLabel="Descartar"
          onConfirm={onClose}
          onCancel={() => setConfirm(null)}
        />
      )}
    </>
  );
}
