import { Eye, EyeOff, Plus, Trash2 } from "lucide-react";
import { Icon } from "@/components/Icon";
import { Button, IconButton } from "@/components/ui";
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
              "min-h-11 rounded-full px-4 text-xs font-bold focus-visible:outline-2 focus-visible:outline-primary disabled:opacity-50",
              tab === t ? "bg-primary-container text-on-primary-container" : "bg-surface-2 text-on-surface-variant hover:bg-surface-3",
            )}
          >
            {TAB_LABELS[t]}
          </button>
        ))}
      </div>
      <div id="zone-tabpanel" role="tabpanel" aria-labelledby={`zone-tab-${tab}`} className="flex min-h-0 flex-col gap-2">
        <Button variant="tonal" disabled={disabled} onClick={onNew}>
          <Icon icon={Plus} size="xs" />
          {tab === "zone" ? "Nueva zona" : "Nueva máscara"}
        </Button>
        {items.length === 0 ? (
          <p className="rounded-m3-lg bg-surface-2 px-3 py-4 text-center text-xs text-muted">{EMPTY[tab]}</p>
        ) : (
          <ul aria-label={TAB_LABELS[tab]} className="flex max-h-56 flex-col gap-1 overflow-auto">
            {items.map((it) => {
              const hasError = (issues[it.uid]?.errors.length ?? 0) > 0;
              const isHidden = hidden.has(it.uid);
              return (
                <li
                  key={it.uid}
                  className={cn(
                    "flex items-center gap-1 rounded-m3-lg px-3 py-1 text-sm",
                    it.uid === selectedUid ? "bg-primary-container text-on-primary-container" : "bg-surface-2",
                  )}
                >
                  <span aria-hidden className="size-3 shrink-0 rounded-sm" style={{ backgroundColor: colorOf(it) }} />
                  <button
                    type="button"
                    disabled={disabled}
                    aria-current={it.uid === selectedUid}
                    onClick={() => onSelect(it.uid)}
                    className="min-h-11 min-w-0 flex-1 truncate text-left focus-visible:outline-2 focus-visible:outline-primary"
                  >
                    {it.name}
                    {it.kind === "object" && <span className="ml-1 text-xs text-muted">({it.scope || "global"})</span>}
                    {hasError && <span className="ml-1 text-xs text-bad">· revisar</span>}
                  </button>
                  <IconButton
                    icon={isHidden ? EyeOff : Eye}
                    aria-label={`${isHidden ? "Mostrar" : "Ocultar"} ${it.name}`}
                    aria-pressed={!isHidden}
                    onClick={() => onToggleHidden(it.uid)}
                  />
                  <IconButton
                    icon={Trash2}
                    aria-label={`Eliminar ${it.name}`}
                    disabled={disabled}
                    onClick={() => onDelete(it.uid)}
                    className="text-bad"
                  />
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
