import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, UserRound } from "lucide-react";
import { type FormEvent, useState } from "react";
import { api, unwrap } from "@/api/client";
import { facePersonsQuery } from "@/api/queries";
import { PersonDetail } from "@/components/faces/PersonDetail";
import { Button, Empty, ErrorNote, Field, LinkButton, TextInput, Textarea } from "@/components/ui";
import { useT } from "@/i18n";

/** PersonsTab lists the registered people with their sync summary and opens a person's detail. */
export function PersonsTab({ canManage }: { canManage: boolean }) {
  const t = useT();
  const qc = useQueryClient();
  const persons = useQuery(facePersonsQuery);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [notes, setNotes] = useState("");

  const create = useMutation({
    mutationFn: async (e: FormEvent) => {
      e.preventDefault();
      return unwrap(await api.POST("/api/v1/faces/persons", { body: { display_name: name.trim(), notes: notes.trim() } }));
    },
    onSuccess: async (created) => {
      await qc.invalidateQueries({ queryKey: ["faces"] });
      setCreating(false);
      setName("");
      setNotes("");
      setSelectedId(created.id);
    },
  });

  const all = persons.data ?? [];
  const selected = all.find((p) => p.id === selectedId);
  if (selected) return <PersonDetail person={selected} canManage={canManage} onBack={() => setSelectedId(null)} />;

  const shown = all.filter((p) => p.display_name.toLowerCase().includes(search.trim().toLowerCase()));
  // Every person is parked as "disabled" on every server when no server has face recognition on.
  const noServerEnabled = all.length > 0 && all.every((p) => p.sync.total > 0 && p.sync.disabled === p.sync.total);

  return (
    <div className="flex flex-col gap-4">
      {noServerEnabled && (
        <div role="note" className="flex flex-wrap items-center justify-between gap-3 rounded-m3-xl bg-warn/12 px-4 py-3 text-sm">
          <span>{t("faces.noServerBanner")}</span>
          <LinkButton to="/servers" variant="tonal">{t("faces.openServers")}</LinkButton>
        </div>
      )}
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="w-full sm:max-w-sm">
          <Field label={t("faces.searchPeople")}>
            <TextInput type="search" value={search} onChange={(e) => setSearch(e.target.value)} />
          </Field>
        </div>
        {canManage && !creating && (
          <Button variant="filled" onClick={() => setCreating(true)}>
            <Plus className="size-4" aria-hidden /> {t("faces.newPerson")}
          </Button>
        )}
      </div>
      {creating && (
        <form className="grid gap-3 rounded-m3-xl bg-surface-1 p-4 sm:grid-cols-2" onSubmit={(e) => create.mutate(e)}>
          <Field label={t("faces.personName")}>
            <TextInput autoFocus required value={name} maxLength={120} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t("faces.personNotes")}>
            <Textarea value={notes} maxLength={2000} rows={2} onChange={(e) => setNotes(e.target.value)} />
          </Field>
          <div className="flex gap-2 sm:col-span-2">
            <Button type="submit" variant="filled" disabled={!name.trim() || create.isPending}>
              {create.isPending ? t("faces.saving") : t("faces.create")}
            </Button>
            <Button variant="text" onClick={() => setCreating(false)}>{t("common.cancel")}</Button>
          </div>
          <div className="sm:col-span-2">
            <ErrorNote error={create.error} />
          </div>
        </form>
      )}
      <ErrorNote error={persons.error} />
      {persons.isSuccess && all.length === 0 && <Empty icon={UserRound}>{t("faces.peopleEmpty")}</Empty>}
      {all.length > 0 && shown.length === 0 && <Empty>{t("faces.peopleNoMatch")}</Empty>}
      {shown.length > 0 && (
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3" aria-label={t("faces.peopleList")}>
          {shown.map((p) => (
            <li key={p.id}>
              <button
                type="button"
                onClick={() => setSelectedId(p.id)}
                className="m3-press flex w-full flex-col gap-1 rounded-m3-xl bg-surface-1 p-4 text-left hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-primary"
              >
                <span className="text-base font-bold">{p.display_name}</span>
                <span className="text-xs text-muted">{t("faces.images", { count: p.image_count })}</span>
                <span className="text-xs text-on-surface-variant">
                  {p.sync.total > 0 ? t("faces.syncSummary", { synced: p.sync.synced, total: p.sync.total }) : t("faces.noSyncYet")}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
