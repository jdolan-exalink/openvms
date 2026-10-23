import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { faceReviewInboxQuery, faceReviewSightingImageUrl, facePersonsQuery } from "@/api/queries";
import { emptyScope, type Scope, ScopeFilters } from "@/components/faces/ScopeFilters";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Button, Checkbox, Empty, ErrorNote, Field, LinkButton, Select, TextInput } from "@/components/ui";
import { useT } from "@/i18n";
import { fmtDateTime } from "@/lib/format";

const NEW_PERSON = "__new__";

type SuggestionTarget = { identityId: string; similarity: number; sightingIds: string[]; representativeId: string };
/** A verdict names one sighting on each side: sighting IDs survive group merges, identity IDs do not. */
type Verdict = { sightingId: string; otherSightingId: string; similarity: number };

/** recordVerdict keeps the operator's decision on a suggested pair; it is also the calibration data. */
async function recordVerdict(decision: "same" | "different", v: Verdict) {
  unwrap(await api.POST("/api/v1/faces/review-inbox/suggestion-decisions", {
    body: { sighting_id: v.sightingId, other_sighting_id: v.otherSightingId, decision, similarity: v.similarity },
  }));
}

function AttemptCard({ attempt, persons, canManage, selected, onSelect, suggestions, onGroupSuggestion, groupingSuggestion }: { attempt: Schemas["FaceReviewSighting"]; persons: Schemas["FacePerson"][]; canManage: boolean; selected: boolean; onSelect: (id: string, selected: boolean) => void; suggestions: SuggestionTarget[]; onGroupSuggestion: (ids: string[], verdict: Verdict) => void; groupingSuggestion: boolean }) {
  const t = useT();
  const qc = useQueryClient();
  const [target, setTarget] = useState("");
  const [newName, setNewName] = useState("");
  const [hint, setHint] = useState<string | null>(null);
  const [confirmReject, setConfirmReject] = useState(false);
  const [dismissed, setDismissed] = useState<Set<string>>(() => new Set());
  const visibleSuggestions = suggestions.filter((sug) => !dismissed.has(sug.identityId));
  const dismiss = useMutation({
    mutationFn: ({ verdict }: { identityId: string; verdict: Verdict }) => recordVerdict("different", verdict),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["faces", "review-inbox"] }),
    // A verdict that was not stored must not leave the suggestion hidden as if it had been.
    onError: (_err, { identityId }) => setDismissed((current) => {
      const next = new Set(current);
      next.delete(identityId);
      return next;
    }),
  });
  const assign = useMutation({
    mutationFn: async () => {
      const body = target === NEW_PERSON ? { new_person_display_name: newName.trim() } : { person_id: target };
      return unwrap(await api.POST("/api/v1/faces/review-inbox/{sightingId}/assign", { params: { path: { sightingId: attempt.id } }, body }));
    },
    onSuccess: async (res) => {
      setHint(t("faces.assigned", { name: res.person.display_name }));
      await qc.invalidateQueries({ queryKey: ["faces"] });
    },
  });
  const reject = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/api/v1/faces/review-inbox/{sightingId}", { params: { path: { sightingId: attempt.id } } })),
    onSuccess: async () => {
      setConfirmReject(false);
      await qc.invalidateQueries({ queryKey: ["faces", "review-inbox"] });
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
    <li className="flex flex-col gap-2 rounded-m3-xl bg-surface-1 p-3">
      {attempt.crop_available ? (
        <img
          src={faceReviewSightingImageUrl(attempt.id)}
          alt={t("faces.attemptAlt", { server: attempt.server_name })}
          loading="lazy"
          className="aspect-square w-full rounded-m3-lg bg-surface-2 object-cover"
        />
      ) : (
        <div className="flex aspect-square items-center justify-center rounded-m3-lg bg-surface-2 p-4 text-center text-sm text-on-surface-variant">
          {attempt.ingest_error || t("faces.cropPending")}
        </div>
      )}
      <p className="break-all font-mono text-xs text-on-surface-variant">{t("faces.unclassifiedId", { id: attempt.id })}</p>
      {attempt.group_id && <p className="break-all font-mono text-xs font-medium text-primary">{t("faces.identityGroupId", { id: attempt.identity_id })}</p>}
      <div className="flex flex-wrap items-center justify-between gap-1 text-xs text-muted">
        <span>{attempt.server_name}</span>
        {attempt.seen_at && <span>{fmtDateTime(attempt.seen_at)}</span>}
        {attempt.score != null && <span>{t("faces.attemptScore", { score: attempt.score })}</span>}
      </div>
      {visibleSuggestions.map((sug) => {
        const verdict: Verdict = { sightingId: attempt.id, otherSightingId: sug.representativeId, similarity: sug.similarity };
        return (
          <div key={sug.identityId} className="flex flex-col gap-1 rounded-m3-lg bg-surface-2 p-2 text-xs">
            <p className="break-all">{t("faces.suggestionMatch", { id: sug.identityId, percent: Math.round(sug.similarity * 100) })}</p>
            {canManage && (
              <div className="flex flex-wrap gap-1">
                <Button variant="tonal" disabled={groupingSuggestion} onClick={() => onGroupSuggestion([attempt.id, ...sug.sightingIds], verdict)}>
                  {t("faces.groupSuggestion")}
                </Button>
                <Button variant="text" onClick={() => {
                  setDismissed((current) => new Set(current).add(sug.identityId));
                  dismiss.mutate({ identityId: sug.identityId, verdict });
                }}>
                  {t("faces.dismissSuggestion")}
                </Button>
              </div>
            )}
          </div>
        );
      })}
      {canManage && attempt.crop_available && (
        <div className="flex flex-col gap-2">
          <Checkbox
            checked={selected}
            onChange={(checked) => onSelect(attempt.id, checked)}
            label={t("faces.groupSelectSighting", { id: attempt.id })}
          />
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
          <Button variant="tonal" disabled={assign.isPending} onClick={submit}>
            {assign.isPending ? t("faces.assigning") : t("faces.assign")}
          </Button>
          {hint && <p role="status" className="text-xs text-on-surface-variant">{hint}</p>}
          <ErrorNote error={assign.error} />
        </div>
      )}
      {canManage && (
        <>
          <Button variant="text" disabled={reject.isPending} onClick={() => setConfirmReject(true)}>
            {t("faces.rejectSighting")}
          </Button>
          <ErrorNote error={reject.error} />
          <ErrorNote error={dismiss.error} />
          {confirmReject && (
            <ConfirmDialog
              title={t("faces.rejectTitle")}
              message={t("faces.rejectMessage")}
              confirmLabel={t("faces.rejectConfirm")}
              pending={reject.isPending}
              error={reject.error}
              onConfirm={() => reject.mutate()}
              onCancel={() => setConfirmReject(false)}
            />
          )}
        </>
      )}
    </li>
  );
}

function suggestionTargets(attempt: Schemas["FaceReviewSighting"], items: Schemas["FaceReviewSighting"][]): SuggestionTarget[] {
  return (attempt.suggestions ?? []).flatMap((sug) => {
    const sightingIds = items.filter((item) => item.identity_id === sug.identity_id).map((item) => item.id);
    const [representativeId] = sightingIds; // the sighting a verdict names on that side
    return representativeId === undefined ? [] : [{ identityId: sug.identity_id, similarity: sug.similarity, sightingIds, representativeId }];
  });
}

/** TrainTab shows the durable inbox of unclassified sightings and their stable identities. */
export function TrainTab({ canManage }: { canManage: boolean }) {
  const t = useT();
  const qc = useQueryClient();
  const [scope, setScope] = useState<Scope>(emptyScope);
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const train = useQuery(faceReviewInboxQuery({ site_id: scope.site ? [scope.site] : undefined, server_id: scope.server ? [scope.server] : undefined }));
  const persons = useQuery(facePersonsQuery());
  const data = train.data;
  const [confirmRejectSelected, setConfirmRejectSelected] = useState(false);
  const group = useMutation({
    mutationFn: async ({ ids, verdict }: { ids: string[]; verdict?: Verdict }) => {
      // The verdict is calibration data, so it is stored first and a failure stops here, visibly,
      // instead of being lost after the grouping went through.
      if (verdict) await recordVerdict("same", verdict);
      return unwrap(await api.POST("/api/v1/faces/review-inbox/groups", { body: { sighting_ids: ids } }));
    },
    onSuccess: async () => {
      setSelected(new Set());
      await qc.invalidateQueries({ queryKey: ["faces", "review-inbox"] });
    },
  });
  const rejectSelected = useMutation({
    mutationFn: async (ids: string[]) => {
      for (const id of ids) unwrap(await api.DELETE("/api/v1/faces/review-inbox/{sightingId}", { params: { path: { sightingId: id } } }));
    },
    onSettled: async () => {
      setConfirmRejectSelected(false);
      setSelected(new Set());
      await qc.invalidateQueries({ queryKey: ["faces", "review-inbox"] });
    },
  });

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-on-surface-variant">{t("faces.trainHint")}</p>
      <ScopeFilters value={scope} onChange={setScope} />
      <ErrorNote error={train.error} />
      {data && data.items.length === 0 && (
        <div className="flex flex-col items-center gap-3">
          <Empty>{t("faces.trainEmpty")}</Empty>
          <LinkButton to="/servers" variant="tonal">{t("faces.openServers")}</LinkButton>
        </div>
      )}
      {data && data.items.length > 0 && (
        <>
          {canManage && <div className="flex flex-col gap-2 rounded-m3-xl bg-surface-1 p-3 sm:flex-row sm:items-center sm:justify-between">
            <span className="text-sm text-on-surface-variant">{t("faces.groupSelectionCount", { count: selected.size })}</span>
            <Button variant="tonal" disabled={selected.size < 2 || group.isPending} onClick={() => group.mutate({ ids: Array.from(selected) })}>
              {group.isPending ? t("faces.grouping") : t("faces.groupSelected")}
            </Button>
            <Button variant="text" disabled={selected.size < 1 || rejectSelected.isPending} onClick={() => setConfirmRejectSelected(true)}>
              {t("faces.rejectSelected")}
            </Button>
            <ErrorNote error={group.error} />
            <ErrorNote error={rejectSelected.error} />
          </div>}
          {confirmRejectSelected && (
            <ConfirmDialog
              title={t("faces.rejectSelectedTitle")}
              message={t("faces.rejectSelectedMessage", { count: selected.size })}
              confirmLabel={t("faces.rejectSelectedConfirm")}
              pending={rejectSelected.isPending}
              error={rejectSelected.error}
              onConfirm={() => rejectSelected.mutate(Array.from(selected))}
              onCancel={() => setConfirmRejectSelected(false)}
            />
          )}
          <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4" aria-label={t("faces.trainList")}>
            {data.items.map((a) => (
              <AttemptCard
                key={a.id}
                attempt={a}
                persons={persons.data ?? []}
                canManage={canManage}
                selected={selected.has(a.id)}
                suggestions={suggestionTargets(a, data.items)}
                onGroupSuggestion={(ids, verdict) => group.mutate({ ids, verdict })}
                groupingSuggestion={group.isPending}
                onSelect={(id, checked) => setSelected((current) => {
                  const next = new Set(current);
                  if (checked) next.add(id);
                  else next.delete(id);
                  return next;
                })}
              />
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
