import { Eye, EyeOff, Plus, RefreshCw, Trash2 } from "lucide-react";
import { Icon } from "@/components/Icon";
import { Button, IconButton } from "@/components/ui";
import { cn } from "@/lib/cn";
import type { ZoneSyncStates, ZoneSyncStatus } from "@/lib/cameraZones";
import { type EditorItem, type ItemIssues, type ItemKind, colorOf } from "./zoneDraft";

export const TAB_LABELS: Record<ItemKind, string> = {
  zone: "Zonas",
  motion: "Máscaras de movimiento",
  object: "Máscaras de objetos",
};

const SYNC_LABELS: Record<ZoneSyncStatus, string> = {
  pending: "Pendiente",
  synced: "Sincronizada",
  failed: "Error de sincronización",
  conflict: "Conflicto",
};

const SYNC_TONES: Record<ZoneSyncStatus, string> = {
  pending: "bg-surface-3 text-on-surface-variant",
  synced: "bg-surface-3 text-ok",
  failed: "bg-surface-3 text-bad",
  conflict: "bg-surface-3 text-bad",
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
  syncStates,
  onRetrySync,
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
  /** Sync state of persisted zones by zone ID; live, independent from the edited draft. */
  syncStates?: ZoneSyncStates;
  /** Present only when the user may retry a failed or conflicted synchronization. */
  onRetrySync?: (zoneId: string) => void;
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
              const sync = it.managedId ? syncStates?.[it.managedId] : undefined;
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
                  {sync && (
                    <span title={sync.error} className={cn("shrink-0 rounded-full px-2 py-0.5 text-[11px] font-bold", SYNC_TONES[sync.status])}>
                      {SYNC_LABELS[sync.status]}
                    </span>
                  )}
                  {sync && it.managedId && onRetrySync && (sync.status === "failed" || sync.status === "conflict") && (
                    <IconButton icon={RefreshCw} aria-label={`Reintentar sincronización de ${it.name}`} onClick={() => onRetrySync(it.managedId!)} />
                  )}
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
