import { X } from "lucide-react";
import { Button } from "@/components/ui";
import { useT } from "@/i18n";

export type FilterChip = { key: string; label: string; onRemove: () => void };

/**
 * SearchSummary reports the outcome of the applied search (live result count) and lists the
 * applied filters as removable chips. It only reflects filters the backend already supports.
 */
export function SearchSummary({
  count,
  noun,
  hasMore,
  loading,
  chips,
  onClear,
}: {
  count: number;
  noun: { one: string; many: string };
  hasMore: boolean;
  loading: boolean;
  chips: FilterChip[];
  onClear: () => void;
}) {
  const t = useT();
  const text = loading ? t("common.searching") : `${count}${hasMore ? "+" : ""} ${count === 1 && !hasMore ? noun.one : noun.many}`;
  return (
    <div className="flex flex-wrap items-center gap-2 text-sm">
      <p role="status" className="text-muted">
        {text}
      </p>
      {chips.length > 0 && (
        <>
          <ul aria-label={t("common.appliedFilters")} className="flex flex-wrap gap-2">
            {chips.map((c) => (
              <li key={c.key} className="inline-flex items-center gap-1 rounded-full border border-line bg-raised py-0.5 pl-2.5 pr-1 text-xs">
                <span>{c.label}</span>
                <button type="button" onClick={c.onRemove} aria-label={t("common.removeFilter", { label: c.label })} className="rounded-full p-0.5 hover:bg-line focus-visible:outline-2 focus-visible:outline-accent">
                  <X className="size-3" aria-hidden />
                </button>
              </li>
            ))}
          </ul>
          <Button onClick={onClear} className="px-2 py-0.5 text-xs">
            {t("common.clearFilters")}
          </Button>
        </>
      )}
    </div>
  );
}
