import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { facePersonsQuery, faceWatchlistsQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Button, Card, Checkbox, Empty, ErrorNote, Field, IconButton, TextInput, Textarea } from "@/components/ui";
import { useT } from "@/i18n";

type Watchlist = Schemas["FaceWatchlist"];
type Person = Schemas["FacePerson"];

/** WatchlistsTab manages named groups of registered people that alarm rules can reference. */
export function WatchlistsTab({ canManage }: { canManage: boolean }) {
  const t = useT();
  const lists = useQuery(faceWatchlistsQuery);
  const persons = useQuery(facePersonsQuery);
  // "new" opens an empty editor; an id opens that list's editor.
  const [editing, setEditing] = useState<string | "new" | null>(null);
  const [deleting, setDeleting] = useState<Watchlist | null>(null);
  const people = persons.data ?? [];
  const nameOf = (id: string) => people.find((p) => p.id === id)?.display_name;

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-on-surface-variant">{t("faces.wlIntro")}</p>
      <ErrorNote error={lists.error} />
      {canManage && editing !== "new" && (
        <div>
          <Button variant="filled" onClick={() => setEditing("new")}>
            <Plus className="size-4" aria-hidden /> {t("faces.wlNew")}
          </Button>
        </div>
      )}
      {canManage && editing === "new" && <Editor people={people} onClose={() => setEditing(null)} />}
      {lists.isSuccess && lists.data.length === 0 && <Empty>{t("faces.wlEmpty")}</Empty>}
      {lists.data && lists.data.length > 0 && (
        <ul className="flex flex-col gap-3" aria-label={t("faces.wlList")}>
          {lists.data.map((w) => (
            <li key={w.id}>
              <Card>
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="flex flex-col gap-0.5">
                    <span className="font-medium">{w.name}</span>
                    {w.description && <span className="text-xs text-muted">{w.description}</span>}
                    <span className="text-sm text-on-surface-variant">
                      {t("faces.wlMemberCount", { n: w.person_ids.length })}
                      {w.person_ids.length > 0 && ` · ${w.person_ids.map((id) => nameOf(id) ?? "…").join(", ")}`}
                    </span>
                  </div>
                  {canManage && (
                    <div className="flex gap-1">
                      <IconButton icon={Pencil} size="sm" variant="tonal" aria-label={t("faces.wlEdit", { name: w.name })} title={t("faces.wlEdit", { name: w.name })} onClick={() => setEditing(editing === w.id ? null : w.id)} />
                      <IconButton icon={Trash2} size="sm" variant="tonal" aria-label={t("faces.wlDelete", { name: w.name })} title={t("faces.wlDelete", { name: w.name })} onClick={() => setDeleting(w)} />
                    </div>
                  )}
                </div>
                {canManage && editing === w.id && <Editor key={w.id} list={w} people={people} onClose={() => setEditing(null)} />}
              </Card>
            </li>
          ))}
        </ul>
      )}
      {deleting && <DeleteDialog list={deleting} onClose={() => setDeleting(null)} />}
    </div>
  );
}

function DeleteDialog({ list, onClose }: { list: Watchlist; onClose: () => void }) {
  const t = useT();
  const qc = useQueryClient();
  const remove = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/api/v1/faces/watchlists/{watchlistId}", { params: { path: { watchlistId: list.id } } })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["faces", "watchlists"] });
      onClose();
    },
  });
  return (
    <ConfirmDialog
      title={t("faces.wlDeleteTitle")}
      message={t("faces.wlDeleteMessage", { name: list.name })}
      confirmLabel={t("faces.wlDeleteTitle")}
      pending={remove.isPending}
      error={remove.error}
      onConfirm={() => remove.mutate()}
      onCancel={onClose}
    />
  );
}

function Editor({ list, people, onClose }: { list?: Watchlist; people: Person[]; onClose: () => void }) {
  const t = useT();
  const qc = useQueryClient();
  const [name, setName] = useState(list?.name ?? "");
  const [description, setDescription] = useState(list?.description ?? "");
  const [members, setMembers] = useState<string[]>(list?.person_ids ?? []);

  const save = useMutation({
    mutationFn: async (e: FormEvent) => {
      e.preventDefault();
      // Members keep the order of the people list so the request is deterministic; ids the
      // people list does not hold (not loaded yet, or it failed) are kept, never dropped.
      const order = new Map(people.map((p, i) => [p.id, i]));
      const ids = [...members].sort((a, b) => (order.get(a) ?? people.length) - (order.get(b) ?? people.length));
      if (!list) {
        return unwrap(await api.POST("/api/v1/faces/watchlists", { body: { name: name.trim(), description: description.trim(), person_ids: ids } }));
      }
      const body: Schemas["FaceWatchlistUpdate"] = {};
      if (name.trim() !== list.name) body.name = name.trim();
      if (description.trim() !== list.description) body.description = description.trim();
      const before = [...list.person_ids].sort().join(",");
      if (before !== [...ids].sort().join(",")) body.person_ids = ids;
      return unwrap(await api.PATCH("/api/v1/faces/watchlists/{watchlistId}", { params: { path: { watchlistId: list.id } }, body }));
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["faces", "watchlists"] });
      onClose();
    },
  });

  return (
    <form className="flex flex-col gap-4 rounded-m3-xl bg-surface-1 p-4" onSubmit={(e) => save.mutate(e)}>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t("faces.wlName")}>
          <TextInput autoFocus required value={name} maxLength={120} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={t("faces.wlDescription")}>
          <Textarea rows={2} value={description} maxLength={500} onChange={(e) => setDescription(e.target.value)} />
        </Field>
      </div>
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 text-sm font-medium">{t("faces.wlMembers")}</legend>
        {people.length === 0 && <span className="text-xs text-muted">{t("faces.wlNoPeople")}</span>}
        <div className="grid max-h-56 gap-2 overflow-y-auto sm:grid-cols-2">
          {people.map((p) => (
            <Checkbox
              key={p.id}
              checked={members.includes(p.id)}
              onChange={(on) => setMembers(on ? [...members, p.id] : members.filter((id) => id !== p.id))}
              label={p.display_name}
            />
          ))}
        </div>
      </fieldset>
      <ErrorNote error={save.error} />
      <div className="flex gap-2">
        <Button type="submit" variant="filled" disabled={!name.trim() || save.isPending}>
          {save.isPending ? t("faces.saving") : list ? t("faces.wlSave") : t("faces.wlCreate")}
        </Button>
        <Button variant="text" onClick={onClose}>{t("faces.wlCancel")}</Button>
      </div>
    </form>
  );
}
