import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { faceTrainImageUrl, faceTrainQuery, facePersonsQuery } from "@/api/queries";
import { Button, Empty, ErrorNote, Field, LinkButton, Select, TextInput } from "@/components/ui";
import { useT } from "@/i18n";
import { fmtDateTime } from "@/lib/format";

const NEW_PERSON = "__new__";

function AttemptCard({ attempt, persons, canManage, tenantId }: { attempt: Schemas["FaceTrainAttempt"]; persons: Schemas["FacePerson"][]; canManage: boolean; tenantId?: string }) {
  const t = useT();
  const qc = useQueryClient();
  const [target, setTarget] = useState("");
  const [newName, setNewName] = useState("");
  const [hint, setHint] = useState<string | null>(null);
  const assign = useMutation({
    mutationFn: async () => {
      const base = { server_id: attempt.server_id, file: attempt.file };
      const body = target === NEW_PERSON ? { ...base, new_person_display_name: newName.trim() } : { ...base, person_id: target };
      return unwrap(await api.POST("/api/v1/faces/train/assign", { params: { query: { tenant_id: tenantId } }, body }));
    },
    onSuccess: async (res) => {
      setHint(t("faces.assigned", { name: res.person.display_name }));
      await qc.invalidateQueries({ queryKey: ["faces"] });
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
      <img
        src={faceTrainImageUrl(attempt.server_id, attempt.file, tenantId)}
        alt={t("faces.attemptAlt", { server: attempt.server_name })}
        loading="lazy"
        className="aspect-square w-full rounded-m3-lg bg-surface-2 object-cover"
      />
      <div className="flex flex-wrap items-center justify-between gap-1 text-xs text-muted">
        <span>{attempt.server_name}</span>
        {attempt.seen_at && <span>{fmtDateTime(attempt.seen_at)}</span>}
        {attempt.score != null && <span>{t("faces.attemptScore", { score: attempt.score })}</span>}
      </div>
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
          <Button variant="tonal" disabled={assign.isPending} onClick={submit}>
            {assign.isPending ? t("faces.assigning") : t("faces.assign")}
          </Button>
          {hint && <p role="status" className="text-xs text-on-surface-variant">{hint}</p>}
          <ErrorNote error={assign.error} />
        </div>
      )}
    </li>
  );
}

/** TrainTab shows Frigate's unclassified attempts so a manager can turn them into enrollment photos. */
export function TrainTab({ canManage, tenantId }: { canManage: boolean; tenantId?: string }) {
  const t = useT();
  const train = useQuery(faceTrainQuery(tenantId));
  const persons = useQuery(facePersonsQuery(tenantId));
  const data = train.data;
  const failing = data?.servers.filter((s) => s.error) ?? [];

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-on-surface-variant">{t("faces.trainHint")}</p>
      <ErrorNote error={train.error} />
      {failing.map((s) => (
        <p key={s.server_id} role="alert" className="rounded-m3-lg bg-warn/12 px-4 py-3 text-sm">
          {t("faces.trainServerError", { server: s.server_name, error: s.error ?? "" })}
        </p>
      ))}
      {data && data.servers.length === 0 && (
        <div className="flex flex-col items-center gap-3">
          <Empty>{t("faces.trainNoServers")}</Empty>
          <LinkButton to="/servers" variant="tonal">{t("faces.openServers")}</LinkButton>
        </div>
      )}
      {data && data.servers.length > 0 && data.items.length === 0 && <Empty>{t("faces.trainEmpty")}</Empty>}
      {data && data.items.length > 0 && (
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4" aria-label={t("faces.trainList")}>
          {data.items.map((a) => (
            <AttemptCard key={`${a.server_id}/${a.file}`} attempt={a} persons={persons.data ?? []} canManage={canManage} />
          ))}
        </ul>
      )}
    </div>
  );
}
