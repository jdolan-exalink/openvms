import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { type Schemas } from "@/api/client";
import { facePersonsQuery, faceReviewSightingImageUrl, faceUnknownClustersQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { emptyScope, type Scope, ScopeFilters } from "@/components/faces/ScopeFilters";
import { Button, Empty, ErrorNote, Field, Pill, Select, TextInput } from "@/components/ui";
import { useT } from "@/i18n";
import { cn } from "@/lib/cn";
import { assignCluster, dismissCluster, PartialAssignError } from "@/lib/faceClusters";
import { fmtDateTime } from "@/lib/format";

const NEW_PERSON = "__new__";
type Cluster = Schemas["FaceUnknownCluster"];
type Sort = "recurrence" | "recent";

function ClusterCard({ cluster, persons, canManage }: { cluster: Cluster; persons: Schemas["FacePerson"][]; canManage: boolean }) {
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
    <li className={cn("flex flex-col gap-3 rounded-m3-xl bg-surface-1 p-3", cluster.recurrent && "border-l-4 border-warn")}>
      <img
        src={faceReviewSightingImageUrl(cluster.best_sighting_id)}
        alt={t("faces.unknownCoverAlt", { server: cluster.server_names[0] ?? "" })}
        loading="lazy"
        className="aspect-square w-full rounded-m3-lg bg-surface-2 object-cover"
      />
      <div className="flex flex-wrap items-center gap-2">
        <Pill tone="neutral">{t("faces.unknownSightings", { count: cluster.sighting_count })}</Pill>
        <Pill tone="neutral">{t("faces.unknownDays", { count: cluster.distinct_days })}</Pill>
        {cluster.recurrent && <Pill tone="warn">{t("faces.unknownRecurrent")}</Pill>}
      </div>
      <p className="text-xs text-muted">{t("faces.unknownSeen", { when: fmtDateTime(cluster.last_seen_at) })}</p>
      <p className="text-xs text-muted">{t("faces.unknownServers", { names: cluster.server_names.join(", ") })}</p>
      {canManage && (
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
  const [minAppearances, setMinAppearances] = useState(3);
  const [windowDays, setWindowDays] = useState(7);
  const clusters = useQuery(
    faceUnknownClustersQuery({
      site_id: scope.site ? [scope.site] : undefined,
      server_id: scope.server ? [scope.server] : undefined,
      min_appearances: minAppearances,
      window_days: windowDays,
    }),
  );
  const persons = useQuery(facePersonsQuery());
  const items = useMemo(() => {
    const list = [...(clusters.data?.items ?? [])];
    if (sort === "recent") list.sort((a, b) => b.last_seen_at.localeCompare(a.last_seen_at));
    else list.sort((a, b) => Number(b.recurrent) - Number(a.recurrent) || b.sighting_count - a.sighting_count);
    return list;
  }, [clusters.data, sort]);

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
          <TextInput type="number" min={1} max={100} value={minAppearances} onChange={(e) => setMinAppearances(Math.min(100, Math.max(1, Number(e.target.value) || 1)))} />
        </Field>
        <Field label={t("faces.unknownWindowDays")}>
          <TextInput type="number" min={1} max={365} value={windowDays} onChange={(e) => setWindowDays(Math.min(365, Math.max(1, Number(e.target.value) || 1)))} />
        </Field>
      </div>
      <ErrorNote error={clusters.error} />
      {clusters.isSuccess && items.length === 0 && <Empty>{t("faces.unknownEmpty")}</Empty>}
      <ul className="grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-3">
        {items.map((cluster) => (
          <ClusterCard key={cluster.id} cluster={cluster} persons={persons.data ?? []} canManage={canManage} />
        ))}
      </ul>
    </div>
  );
}
