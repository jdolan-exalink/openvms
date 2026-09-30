import { Eye, EyeOff, Plus, Trash2 } from "lucide-react";
import { cn } from "@/lib/cn";
import { type EditorItem, type ItemIssues, type ItemKind, colorOf } from "./zoneDraft";

export const TAB_LABELS: Record<ItemKind, string> = {
  zone: "Zonas",
  motion: "Máscaras de movimiento",
  object: "Máscaras de objetos",
};

const EMPTY: Record<ItemKind, string> = {
  zone: "Esta cámara no tiene zonas.",
  motion: "Sin máscaras de movimiento.",
  object: "Sin máscaras de objetos.",
};

export function ZoneList({
  tab,
  onTab,
  items,
  selectedUid,
  hidden,
  issues,
  disabled,
  onSelect,
  onToggleHidden,
  onDelete,
  onNew,
}: {
  tab: ItemKind;
  onTab: (t: ItemKind) => void;
  items: EditorItem[];
  selectedUid: string | null;
  hidden: Set<string>;
  issues: Record<string, ItemIssues>;
  /** True while a polygon is being drawn. */
  disabled: boolean;
  onSelect: (uid: string) => void;
  onToggleHidden: (uid: string) => void;
  onDelete: (uid: string) => void;
  onNew: () => void;
}) {
  const tabs = Object.keys(TAB_LABELS) as ItemKind[];
  return (
    <div className="flex min-h-0 flex-col gap-2">
      <div role="tablist" aria-label="Tipo de elemento" className="flex flex-wrap gap-1">
        {tabs.map((t) => (
          <button
            key={t}
            type="button"
            role="tab"
            id={`zone-tab-${t}`}
            aria-selected={tab === t}
            aria-controls="zone-tabpanel"
            disabled={disabled}
            onClick={() => onTab(t)}
            className={cn(
              "rounded px-2 py-1 text-xs font-medium focus-visible:outline-2 focus-visible:outline-accent",
              tab === t ? "bg-accent text-bg" : "border border-line hover:bg-raised",
            )}
          >
            {TAB_LABELS[t]}
          </button>
        ))}
      </div>
      <div id="zone-tabpanel" role="tabpanel" aria-labelledby={`zone-tab-${tab}`} className="flex min-h-0 flex-col gap-2">
        <button
          type="button"
          disabled={disabled}
          onClick={onNew}
          className="inline-flex items-center justify-center gap-2 rounded border border-line px-3 py-1.5 text-sm hover:bg-raised disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-accent"
        >
          <Plus className="size-4" aria-hidden />
          {tab === "zone" ? "Nueva zona" : "Nueva máscara"}
        </button>
        {items.length === 0 ? (
          <p className="rounded border border-dashed border-line px-3 py-4 text-center text-xs text-muted">{EMPTY[tab]}</p>
        ) : (
          <ul aria-label={TAB_LABELS[tab]} className="flex max-h-56 flex-col gap-1 overflow-auto">
            {items.map((it) => {
              const hasError = (issues[it.uid]?.errors.length ?? 0) > 0;
              const isHidden = hidden.has(it.uid);
              return (
                <li
                  key={it.uid}
                  className={cn(
                    "flex items-center gap-1 rounded border px-2 py-1 text-sm",
                    it.uid === selectedUid ? "border-accent bg-raised" : "border-line",
                  )}
                >
                  <span aria-hidden className="size-3 shrink-0 rounded-sm" style={{ backgroundColor: colorOf(it) }} />
                  <button
                    type="button"
                    disabled={disabled}
                    aria-current={it.uid === selectedUid}
                    onClick={() => onSelect(it.uid)}
                    className="min-w-0 flex-1 truncate text-left focus-visible:outline-2 focus-visible:outline-accent"
                  >
                    {it.name}
                    {it.kind === "object" && <span className="ml-1 text-xs text-muted">({it.scope || "global"})</span>}
                    {hasError && <span className="ml-1 text-xs text-bad">· revisar</span>}
                  </button>
                  <button
                    type="button"
                    aria-label={`${isHidden ? "Mostrar" : "Ocultar"} ${it.name}`}
                    aria-pressed={!isHidden}
                    onClick={() => onToggleHidden(it.uid)}
                    className="rounded p-1 hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent"
                  >
                    {isHidden ? <EyeOff className="size-4" aria-hidden /> : <Eye className="size-4" aria-hidden />}
                  </button>
                  <button
                    type="button"
                    aria-label={`Eliminar ${it.name}`}
                    disabled={disabled}
                    onClick={() => onDelete(it.uid)}
                    className="rounded p-1 text-bad hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent"
                  >
                    <Trash2 className="size-4" aria-hidden />
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
