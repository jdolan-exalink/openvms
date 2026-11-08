import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { type Schemas } from "@/api/client";
import { facePersonsQuery, faceReviewSightingImageUrl, faceUnknownClustersQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { FaceZoom } from "@/components/faces/FaceZoom";
import { emptyScope, type Scope, ScopeFilters } from "@/components/faces/ScopeFilters";
import { Button, Checkbox, Empty, ErrorNote, Field, Pill, Select, TextInput } from "@/components/ui";
import { useT } from "@/i18n";
import { cn } from "@/lib/cn";
import { assignCluster, dismissCluster, PartialAssignError } from "@/lib/faceClusters";
import { fmtDateTime } from "@/lib/format";

const NEW_PERSON = "__new__";
type Cluster = Schemas["FaceUnknownCluster"];
type Sort = "recurrence" | "recent";

function ClusterCard({ cluster, persons, canManage, selected, onToggle }: {
  cluster: Cluster;
  persons: Schemas["FacePerson"][];
  canManage: boolean;
  /** Set only while selecting: the card then shows a checkbox and its own actions step aside. */
  selected?: boolean;
  onToggle?: () => void;
}) {
  const t = useT();
  const qc = useQueryClient();
  const [target, setTarget] = useState("");
  const [newName, setNewName] = useState("");
  const [hint, setHint] = useState<string | null>(null);
  const [confirmDismiss, setConfirmDismiss] = useState(false);
  const refresh = () => qc.invalidateQueries({ queryKey: ["faces"] });

  const assign = useMutation({
    mutationFn: () => assignCluster(cluster.sighting_ids, target === NEW_PERSON ? { newName } : { personId: target }),
    onSuccess: async (res) => {
      setHint(t("faces.unknownNamed", { name: res.name, count: res.count }));
      await refresh();
    },
    onError: async (err) => {
      if (err instanceof PartialAssignError) setHint(t("faces.unknownPartial", { done: err.done, total: err.total }));
      await refresh();
    },
  });
  const dismiss = useMutation({
    mutationFn: () => dismissCluster(cluster.sighting_ids),
    onSuccess: async () => {
      setConfirmDismiss(false);
      await refresh();
    },
  });

  function submit() {
    if (!target || (target === NEW_PERSON && !newName.trim())) {
      setHint(t("faces.assignMissing"));
      return;
    }
    setHint(null);
    assign.mutate();
  }

  return (
    <li className={cn("flex flex-col gap-3 rounded-m3-xl bg-surface-1 p-3", cluster.recurrent && "border-l-4 border-warn", selected && "ring-2 ring-primary")}>
      {onToggle && <Checkbox checked={selected ?? false} onChange={onToggle} label={t("faces.unknownSelectGroup")} />}
      <FaceZoom
        src={faceReviewSightingImageUrl(cluster.best_sighting_id)}
        alt={t("faces.unknownCoverAlt", { server: cluster.server_names[0] ?? "" })}
        title={t("faces.unknownSightings", { count: cluster.sighting_count })}
        className="aspect-square w-full rounded-m3-lg"
      />
      <div className="flex flex-wrap items-center gap-2">
        <Pill tone="neutral">{t("faces.unknownSightings", { count: cluster.sighting_count })}</Pill>
        <Pill tone="neutral">{t("faces.unknownDays", { count: cluster.distinct_days })}</Pill>
        {cluster.recurrent && <Pill tone="warn">{t("faces.unknownRecurrent")}</Pill>}
      </div>
      <p className="text-xs text-muted">{t("faces.unknownSeen", { when: fmtDateTime(cluster.last_seen_at) })}</p>
      <p className="text-xs text-muted">{t("faces.unknownServers", { names: cluster.server_names.join(", ") })}</p>
      {canManage && !onToggle && (
        <div className="flex flex-col gap-2">
          <Field label={t("faces.assignTo")}>
            <Select value={target} onChange={(e) => setTarget(e.target.value)}>
              <option value="">{t("faces.choosePerson")}</option>
              {persons.map((p) => (
                <option key={p.id} value={p.id}>{p.display_name}</option>
              ))}
              <option value={NEW_PERSON}>{t("faces.newPersonOption")}</option>
            </Select>
          </Field>
          {target === NEW_PERSON && (
            <Field label={t("faces.newPersonName")}>
              <TextInput value={newName} maxLength={120} onChange={(e) => setNewName(e.target.value)} />
            </Field>
          )}
          <div className="flex flex-wrap gap-2">
            <Button variant="tonal" disabled={assign.isPending} onClick={submit}>
              {assign.isPending ? t("faces.assigning") : t("faces.unknownName")}
            </Button>
            <Button variant="text" onClick={() => setConfirmDismiss(true)}>{t("faces.unknownDismiss")}</Button>
          </div>
          {hint && <p role="status" className="text-xs text-on-surface-variant">{hint}</p>}
          <ErrorNote error={assign.error instanceof PartialAssignError ? null : assign.error} />
        </div>
      )}
      {confirmDismiss && (
        <ConfirmDialog
          title={t("faces.unknownDismissTitle")}
          message={t("faces.unknownDismissBody", { count: cluster.sighting_count })}
          confirmLabel={t("faces.unknownDismiss")}
          pending={dismiss.isPending}
          error={dismiss.error}
          onConfirm={() => dismiss.mutate()}
          onCancel={() => setConfirmDismiss(false)}
        />
      )}
    </li>
  );
}

/**
 * Unknown faces grouped by similarity. The operator names a group once instead of reviewing each face;
 * the grouping is only a proposal and nothing changes until they act.
 */
export function UnknownsTab({ canManage }: { canManage: boolean }) {
  const t = useT();
  const [scope, setScope] = useState<Scope>(emptyScope);
  const [sort, setSort] = useState<Sort>("recurrence");
  // null means "the saved platform rule"; the page only overrides it once the operator types a value.
  const [minAppearances, setMinAppearances] = useState<number | null>(null);
  const [windowDays, setWindowDays] = useState<number | null>(null);
  const clusters = useQuery(
    faceUnknownClustersQuery({
      site_id: scope.site ? [scope.site] : undefined,
      server_id: scope.server ? [scope.server] : undefined,
      min_appearances: minAppearances ?? undefined,
      window_days: windowDays ?? undefined,
    }),
  );
  const persons = useQuery(facePersonsQuery());
  const qc = useQueryClient();
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleteHint, setDeleteHint] = useState<string | null>(null);
  const items = useMemo(() => {
    const list = [...(clusters.data?.items ?? [])];
    if (sort === "recent") list.sort((a, b) => b.last_seen_at.localeCompare(a.last_seen_at));
    else list.sort((a, b) => Number(b.recurrent) - Number(a.recurrent) || b.sighting_count - a.sighting_count);
    return list;
  }, [clusters.data, sort]);

  const chosen = items.filter((c) => selected.has(c.id));
  const chosenSightings = chosen.reduce((n, c) => n + c.sighting_count, 0);
  const deleteSelected = useMutation({
    mutationFn: async () => {
      // One group at a time so a failure leaves the rest selected for a retry.
      const done: string[] = [];
      try {
        for (const c of chosen) {
          await dismissCluster(c.sighting_ids);
          done.push(c.id);
        }
      } catch (err) {
        throw Object.assign(err instanceof Error ? err : new Error(String(err)), { done: done.length });
      }
    },
    onSettled: async (_data, err) => {
      setConfirmDelete(false);
      await qc.invalidateQueries({ queryKey: ["faces"] });
      if (err) {
        setDeleteHint(t("faces.unknownDeletePartial", { done: (err as { done?: number }).done ?? 0, total: chosen.length }));
        return;
      }
      setDeleteHint(null);
      setSelected(new Set());
      setSelecting(false);
    },
  });
  const toggle = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-on-surface-variant">{t("faces.unknownIntro")}</p>
      <ScopeFilters value={scope} onChange={setScope} />
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex gap-1" role="group" aria-label={t("faces.unknownSortRecurrence")}>
          <Button variant={sort === "recurrence" ? "tonal" : "text"} aria-pressed={sort === "recurrence"} onClick={() => setSort("recurrence")}>
            {t("faces.unknownSortRecurrence")}
          </Button>
          <Button variant={sort === "recent" ? "tonal" : "text"} aria-pressed={sort === "recent"} onClick={() => setSort("recent")}>
            {t("faces.unknownSortRecent")}
          </Button>
        </div>
        <Field label={t("faces.unknownMinAppearances")}>
          <TextInput type="number" min={1} max={100} value={minAppearances ?? clusters.data?.rule.min_appearances ?? ""} onChange={(e) => setMinAppearances(Math.min(100, Math.max(1, Number(e.target.value) || 1)))} />
        </Field>
        <Field label={t("faces.unknownWindowDays")}>
          <TextInput type="number" min={1} max={365} value={windowDays ?? clusters.data?.rule.window_days ?? ""} onChange={(e) => setWindowDays(Math.min(365, Math.max(1, Number(e.target.value) || 1)))} />
        </Field>
      </div>
      {canManage && items.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant={selecting ? "tonal" : "outlined"}
            aria-pressed={selecting}
            onClick={() => {
              setSelecting(!selecting);
              setSelected(new Set());
              setDeleteHint(null);
            }}
          >
            {selecting ? t("faces.unknownSelectDone") : t("faces.unknownSelect")}
          </Button>
          {selecting && (
            <>
              <Button variant="text" onClick={() => setSelected(selected.size === items.length ? new Set() : new Set(items.map((c) => c.id)))}>
                {selected.size === items.length ? t("faces.unknownSelectNone") : t("faces.unknownSelectAll")}
              </Button>
              <span role="status" className="text-sm text-on-surface-variant">{t("faces.unknownSelectedCount", { count: selected.size })}</span>
              <Button variant="danger" disabled={selected.size === 0} onClick={() => setConfirmDelete(true)}>
                {t("faces.unknownDeleteSelected")}
              </Button>
            </>
          )}
        </div>
      )}
      {deleteHint && <p role="status" className="text-sm text-on-surface-variant">{deleteHint}</p>}
      <ErrorNote error={clusters.error} />
      {clusters.isSuccess && items.length === 0 && <Empty>{t("faces.unknownEmpty")}</Empty>}
      <ul className="grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-3">
        {items.map((cluster) => (
          <ClusterCard
            key={cluster.id}
            cluster={cluster}
            persons={persons.data ?? []}
            canManage={canManage}
            selected={selecting ? selected.has(cluster.id) : undefined}
            onToggle={selecting ? () => toggle(cluster.id) : undefined}
          />
        ))}
      </ul>
      {confirmDelete && (
        <ConfirmDialog
          title={t("faces.unknownDeleteSelectedTitle", { count: chosen.length })}
          message={t("faces.unknownDeleteSelectedBody", { groups: chosen.length, count: chosenSightings })}
          confirmLabel={t("faces.unknownDeleteSelected")}
          pending={deleteSelected.isPending}
          error={deleteSelected.error}
          onConfirm={() => deleteSelected.mutate()}
          onCancel={() => setConfirmDelete(false)}
        />
      )}
    </div>
  );
}
