import { Button, RemovableChip } from "@/components/ui";
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
              <li key={c.key}>
                <RemovableChip label={c.label} removeLabel={t("common.removeFilter", { label: c.label })} onRemove={c.onRemove} />
              </li>
            ))}
          </ul>
          <Button variant="text" size="sm" onClick={onClear}>
            {t("common.clearFilters")}
          </Button>
        </>
      )}
    </div>
  );
}
