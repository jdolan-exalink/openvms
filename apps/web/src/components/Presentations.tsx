import { Fullscreen, Shrink, Trash2 } from "lucide-react";
import { type CSSProperties, useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Button } from "@/components/ui";
import { useT } from "@/i18n";
import { Modal } from "@/components/Modal";
import { cn } from "@/lib/cn";
import {
  clampGrid, DEFAULT_PRESENTATIONS, divisionName, gridSegments, MAX_GRID, samePanes, toggleSegment, uniformPresentation,
  type GridSegment, type Presentation,
} from "@/lib/presentations";

const toolButton =
  "inline-flex size-8 shrink-0 items-center justify-center rounded border border-line bg-surface hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent";

/** presentationId works on plain HTTP, where crypto.randomUUID is missing. */
function presentationId(): string {
  try {
    const id = globalThis.crypto?.randomUUID?.();
    if (id) return `c-${id}`;
  } catch {
    // Insecure origins throw instead of omitting the method.
  }
  return `c-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

/** LayoutGlyph draws one presentation as the small blue window used in the layout menu. */
export function LayoutGlyph({ presentation, className }: { presentation: Pick<Presentation, "columns" | "rows" | "panes">; className?: string }) {
  return (
    <span
      aria-hidden
      className={cn("grid size-5 shrink-0 gap-px rounded-[2px] bg-sky-950/40 p-px", className)}
      style={{ gridTemplateColumns: `repeat(${presentation.columns}, minmax(0, 1fr))`, gridTemplateRows: `repeat(${presentation.rows}, minmax(0, 1fr))` }}
    >
      {presentation.panes.map((pane, index) => (
        <span key={index} className="rounded-[1px] bg-sky-400" style={{ gridColumn: `${pane.col + 1} / span ${pane.colSpan}`, gridRow: `${pane.row + 1} / span ${pane.rowSpan}` }} />
      ))}
    </span>
  );
}

function segmentStyle(segment: GridSegment, columns: number, rows: number): CSSProperties {
  const hit = 14;
  if (segment.orientation === "v") {
    return {
      left: `calc(${((segment.col + 1) / columns) * 100}% - ${hit / 2}px)`,
      top: `${(segment.row / rows) * 100}%`,
      width: hit,
      height: `${(1 / rows) * 100}%`,
    };
  }
  return {
    top: `calc(${((segment.row + 1) / rows) * 100}% - ${hit / 2}px)`,
    left: `${(segment.col / columns) * 100}%`,
    height: hit,
    width: `${(1 / columns) * 100}%`,
  };
}

/** Road-marking colors (white, yellow, black outline) model physical pavement lines, so they are theme-independent. */
function LineMark({ segment }: { segment: GridSegment }) {
  const vertical = segment.orientation === "v";
  return (
    <>
      <span
        className={cn(
          "pointer-events-none absolute rounded-sm bg-black",
          vertical ? "inset-y-0 left-1/2 w-2 -translate-x-1/2" : "inset-x-0 top-1/2 h-2 -translate-y-1/2",
        )}
      />
      <span
        className={cn(
          "pointer-events-none absolute rounded-sm",
          vertical ? "inset-y-0 left-1/2 w-1.5 -translate-x-1/2" : "inset-x-0 top-1/2 h-1.5 -translate-y-1/2",
          segment.solid
            ? "bg-white shadow-[0_0_0_1px_#fff,0_0_6px_rgba(255,255,255,0.85)]"
            : vertical
              ? "bg-[repeating-linear-gradient(180deg,#fbbf24_0_9px,transparent_9px_14px)] shadow-[0_0_6px_rgba(251,191,36,0.9)]"
              : "bg-[repeating-linear-gradient(90deg,#fbbf24_0_9px,transparent_9px_14px)] shadow-[0_0_6px_rgba(251,191,36,0.9)]",
        )}
      />
    </>
  );
}

function PresentationCanvas({ presentation, locked, onToggle }: { presentation: Presentation; locked?: boolean; onToggle: (segment: GridSegment) => void }) {
  const t = useT();
  const segments = gridSegments(presentation.columns, presentation.rows, presentation.panes);
  return (
    <div className="relative mx-auto aspect-square w-full max-w-xl overflow-hidden rounded-md border border-line bg-slate-950">
      <div
        className="grid size-full gap-px bg-black"
        style={{ gridTemplateColumns: `repeat(${presentation.columns}, minmax(0, 1fr))`, gridTemplateRows: `repeat(${presentation.rows}, minmax(0, 1fr))` }}
      >
        {presentation.panes.map((pane, index) => (
          <div key={index} className="bg-sky-500/30" style={{ gridColumn: `${pane.col + 1} / span ${pane.colSpan}`, gridRow: `${pane.row + 1} / span ${pane.rowSpan}` }} />
        ))}
      </div>
      {segments.map((segment) => (
        <button
          key={`${segment.orientation}-${segment.col}-${segment.row}`}
          type="button"
          disabled={locked}
          aria-label={segment.solid ? t("live.removeLine") : t("live.restoreLine")}
          title={segment.solid ? t("live.removeLine") : t("live.restoreLine")}
          className="absolute z-10 hover:bg-white/15 focus-visible:outline-2 focus-visible:outline-accent disabled:cursor-default disabled:hover:bg-transparent"
          style={segmentStyle(segment, presentation.columns, presentation.rows)}
          onClick={() => { if (!locked) onToggle(segment); }}
        >
          <LineMark segment={segment} />
        </button>
      ))}
    </div>
  );
}

function LatticeField({ label, value, disabled, onChange }: { label: string; value: number; disabled?: boolean; onChange: (value: number) => void }) {
  return (
    <label className={cn("flex items-center gap-2 text-sm", disabled && "text-muted")}>
      {label}
      <input
        aria-label={label}
        type="number"
        min={1}
        max={MAX_GRID}
        disabled={disabled}
        value={value}
        onChange={(event) => {
          const next = Number(event.target.value);
          if (Number.isInteger(next)) onChange(clampGrid(next));
        }}
        className="w-16 rounded border border-line bg-bg px-2 py-1 text-center tabular-nums focus-visible:outline-2 focus-visible:outline-accent disabled:opacity-60"
      />
    </label>
  );
}

/** PresentationEditor is the Avigilon-style dialog: pick a shape, set columns and rows, delete lines. */
export function PresentationEditor({
  catalog,
  active,
  onCancel,
  onAccept,
}: {
  catalog: Presentation[];
  active: PaneMatch;
  onCancel: () => void;
  onAccept: (catalog: Presentation[], selectedId: string) => void;
}) {
  const [draft, setDraft] = useState(catalog);
  const [selectedId, setSelectedId] = useState(() => catalog.find((item) => samePanes(item.panes, active.panes) && item.columns === active.columns)?.id ?? catalog[0]?.id ?? "");
  const t = useT();
  const selected = draft.find((item) => item.id === selectedId) ?? draft[0];
  const standards = draft.filter((item) => item.builtin);
  const customs = draft.filter((item) => !item.builtin);
  const locked = Boolean(selected?.builtin);

  const retitle = (presentation: Presentation): Presentation => ({ ...presentation, name: divisionName(presentation.panes.length), builtin: false });
  const replace = (next: Presentation) => setDraft((list) => list.map((item) => (item.id === next.id ? next : item)));
  const createCustom = () => {
    const source = selected ?? DEFAULT_PRESENTATIONS[2] ?? DEFAULT_PRESENTATIONS[0];
    if (!source) return;
    const next: Presentation = { ...source, id: presentationId(), builtin: false, name: divisionName(source.panes.length) };
    setDraft((list) => [...list, next]);
    setSelectedId(next.id);
  };
  const removeCustom = (id: string) => {
    setDraft((list) => list.filter((item) => item.id !== id));
    if (selectedId === id) setSelectedId(DEFAULT_PRESENTATIONS[0]?.id ?? "");
  };

  if (!selected) return null;
  const renderRow = (item: Presentation) => (
    <li key={item.id} className="flex items-stretch">
      <button
        type="button"
        role="option"
        aria-selected={item.id === selected.id}
        onClick={() => setSelectedId(item.id)}
        className={cn("flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-left text-sm", item.id === selected.id ? "bg-accent text-bg" : "hover:bg-raised")}
      >
        <LayoutGlyph presentation={item} className={item.id === selected.id ? "bg-bg/20" : undefined} />
        <span className="truncate">{item.name}</span>
      </button>
      {!item.builtin && (
        <button
          type="button"
          aria-label={t("common.deleteNamed", { name: item.name })}
          title={t("common.deleteNamed", { name: item.name })}
          onClick={() => removeCustom(item.id)}
          className="px-2 text-muted hover:bg-raised hover:text-fg"
        >
          <Trash2 className="size-3.5" aria-hidden />
        </button>
      )}
    </li>
  );
  return (
    <Modal title={t("live.edit")} onClose={onCancel} className="max-w-4xl">
      <div className="grid items-start gap-4 md:grid-cols-[16rem_1fr]">
        <div className="min-w-0">
          <div className="mb-1 flex items-center justify-between gap-2">
            <p className="text-xs font-medium text-muted">{t("live.presentations")}</p>
            <Button onClick={createCustom}>{t("common.new")}</Button>
          </div>
          <ul role="listbox" aria-label={t("live.presentations")} className="max-h-80 overflow-auto rounded border border-line">
            <li className="px-2 py-1 text-[11px] font-medium uppercase tracking-wide text-muted">{t("live.standard")}</li>
            {standards.map(renderRow)}
            <li className="border-t border-line px-2 py-1 text-[11px] font-medium uppercase tracking-wide text-muted">{t("live.custom")}</li>
            {customs.length === 0 && <li className="px-2 py-2 text-xs text-muted">{t("live.noCustom")}</li>}
            {customs.map(renderRow)}
          </ul>
        </div>
        <div className="flex min-w-0 flex-col gap-3">
          <div className="flex flex-wrap items-center justify-center gap-4">
            <LatticeField label={t("live.columns")} value={selected.columns} disabled={locked} onChange={(columns) => replace(retitle({ ...uniformPresentation(columns, selected.rows, selected.id), builtin: false }))} />
            <LatticeField label={t("live.rows")} value={selected.rows} disabled={locked} onChange={(rows) => replace(retitle({ ...uniformPresentation(selected.columns, rows, selected.id), builtin: false }))} />
          </div>
          <PresentationCanvas presentation={selected} locked={locked} onToggle={(segment) => replace(retitle({ ...selected, panes: toggleSegment(selected.panes, segment) }))} />
          <p className="text-center text-xs text-muted">
            {locked
              ? t("live.standardLocked")
              : t("live.lineHint")}
          </p>
        </div>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Button
          disabled={customs.length === 0}
          onClick={() => { setDraft(DEFAULT_PRESENTATIONS); setSelectedId(DEFAULT_PRESENTATIONS[0]?.id ?? ""); }}
        >
          {t("live.removeCustom")}
        </Button>
        <div className="flex gap-2">
          <Button onClick={onCancel}>{t("common.cancel")}</Button>
          <Button variant="primary" onClick={() => onAccept(draft, selected.id)}>{t("common.accept")}</Button>
        </div>
      </div>
    </Modal>
  );
}

type PaneMatch = Pick<Presentation, "columns" | "panes">;

/** LayoutMenu is the toolbar dropdown of presentations plus the entry to the editor. */
export function LayoutMenu({
  catalog,
  active,
  onSelect,
  onEdit,
}: {
  catalog: Presentation[];
  active: PaneMatch;
  onSelect: (presentation: Presentation) => void;
  onEdit: () => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [menuBox, setMenuBox] = useState<{ top: number; left: number } | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const menuId = useId();
  const current = catalog.find((item) => item.columns === active.columns && samePanes(item.panes, active.panes));

  const toggleMenu = () => {
    if (open) {
      setOpen(false);
      return;
    }
    const rect = rootRef.current?.getBoundingClientRect();
    if (rect) setMenuBox({ top: rect.bottom + 4, left: rect.left });
    setOpen(true);
  };

  useEffect(() => {
    if (!open) return;
    const onPointer = (event: MouseEvent) => {
      const target = event.target as Node;
      if (rootRef.current?.contains(target) || menuRef.current?.contains(target)) return;
      setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={menuId}
        aria-label={t("live.presentation")}
        title={current?.name ?? t("live.presentation")}
        onClick={toggleMenu}
        className={cn(toolButton, open && "border-accent")}
      >
        <LayoutGlyph presentation={current ?? { columns: active.columns, rows: Math.max(...active.panes.map((pane) => pane.row + pane.rowSpan), 1), panes: active.panes }} className="size-4 p-0" />
      </button>
      {open && menuBox && createPortal(
        <div ref={menuRef} id={menuId} role="menu" aria-label={t("live.presentations")} className="fixed z-[70] max-h-80 w-52 overflow-auto rounded border border-line bg-surface py-1 shadow-lg" style={{ top: menuBox.top, left: menuBox.left }}>
          {catalog.filter((item) => item.builtin).map((item) => {
            const selected = item.id === current?.id;
            return (
              <button
                key={item.id}
                type="button"
                role="menuitem"
                aria-current={selected ? "true" : undefined}
                onClick={() => { onSelect(item); setOpen(false); }}
                className={cn("flex w-full items-center gap-2 px-2 py-1.5 text-left text-sm", selected ? "bg-accent text-bg" : "hover:bg-raised")}
              >
                <LayoutGlyph presentation={item} className={selected ? "bg-bg/20" : undefined} />
                <span className="truncate">{item.name}</span>
              </button>
            );
          })}
          {catalog.some((item) => !item.builtin) && (
            <>
              <div className="my-1 border-t border-line px-2 pt-1 text-[11px] font-medium uppercase tracking-wide text-muted">{t("live.custom")}</div>
              {catalog.filter((item) => !item.builtin).map((item) => {
                const selected = item.id === current?.id;
                return (
                  <button
                    key={item.id}
                    type="button"
                    role="menuitem"
                    aria-current={selected ? "true" : undefined}
                    onClick={() => { onSelect(item); setOpen(false); }}
                    className={cn("flex w-full items-center gap-2 px-2 py-1.5 text-left text-sm", selected ? "bg-accent text-bg" : "hover:bg-raised")}
                  >
                    <LayoutGlyph presentation={item} className={selected ? "bg-bg/20" : undefined} />
                    <span className="truncate">{item.name}</span>
                  </button>
                );
              })}
            </>
          )}
          <div className="my-1 border-t border-line" />
          <button
            type="button"
            role="menuitem"
            onClick={() => { setOpen(false); onEdit(); }}
            className="w-full px-3 py-1.5 text-left text-sm hover:bg-raised"
          >
            {t("live.editEllipsis")}
          </button>
        </div>,
        document.body,
      )}
    </div>
  );
}

export function FullscreenButton({ active, onClick }: { active: boolean; onClick: () => void }) {
  const t = useT();
  const Icon = active ? Shrink : Fullscreen;
  return (
    <button
      type="button"
      aria-pressed={active}
      aria-label={active ? t("live.exitFullscreen") : t("live.fullscreen")}
      title={active ? t("live.exitFullscreen") : t("live.fullscreen")}
      onClick={onClick}
      className={toolButton}
    >
      <Icon className="size-4" aria-hidden />
    </button>
  );
}
