import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { faceReviewInboxQuery, faceReviewSightingImageUrl, facePersonsQuery } from "@/api/queries";
import { emptyScope, type Scope, ScopeFilters } from "@/components/faces/ScopeFilters";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Button, Empty, ErrorNote, Field, LinkButton, Select, TextInput } from "@/components/ui";
import { useT } from "@/i18n";
import { fmtDateTime } from "@/lib/format";

const NEW_PERSON = "__new__";

function AttemptCard({ attempt, persons, canManage }: { attempt: Schemas["FaceReviewSighting"]; persons: Schemas["FacePerson"][]; canManage: boolean }) {
  const t = useT();
  const qc = useQueryClient();
  const [target, setTarget] = useState("");
  const [newName, setNewName] = useState("");
  const [hint, setHint] = useState<string | null>(null);
  const [confirmReject, setConfirmReject] = useState(false);
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
      <div className="flex flex-wrap items-center justify-between gap-1 text-xs text-muted">
        <span>{attempt.server_name}</span>
        {attempt.seen_at && <span>{fmtDateTime(attempt.seen_at)}</span>}
        {attempt.score != null && <span>{t("faces.attemptScore", { score: attempt.score })}</span>}
      </div>
      {canManage && attempt.crop_available && (
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

/** TrainTab shows the durable inbox of unclassified sightings and their stable identities. */
export function TrainTab({ canManage }: { canManage: boolean }) {
  const t = useT();
  const [scope, setScope] = useState<Scope>(emptyScope);
  const train = useQuery(faceReviewInboxQuery({ site_id: scope.site ? [scope.site] : undefined, server_id: scope.server ? [scope.server] : undefined }));
  const persons = useQuery(facePersonsQuery());
  const data = train.data;

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
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4" aria-label={t("faces.trainList")}>
          {data.items.map((a) => (
            <AttemptCard key={a.id} attempt={a} persons={persons.data ?? []} canManage={canManage} />
          ))}
        </ul>
      )}
    </div>
  );
}
