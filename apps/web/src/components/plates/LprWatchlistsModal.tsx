import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronUp, Pencil, Plus, ShieldAlert, Trash2 } from "lucide-react";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { lprWatchlistItemsQuery, lprWatchlistsQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Modal } from "@/components/Modal";
import { Button, Card, Empty, ErrorNote, Field, IconButton, TextInput, Textarea } from "@/components/ui";
import { ArPlate } from "@/components/plates/ArPlate";
import { useT } from "@/i18n";

type Watchlist = Schemas["LprWatchlist"];
type WatchlistItem = Schemas["LprWatchlistItem"];

const PRESET_COLORS = [
  { label: "Rojo", value: "#ef4444" },
  { label: "Ámbar", value: "#f59e0b" },
  { label: "Verde", value: "#10b981" },
  { label: "Azul", value: "#3b82f6" },
  { label: "Púrpura", value: "#8b5cf6" },
  { label: "Rosa", value: "#ec4899" },
];

export function LprWatchlistsModal({
  canManage,
  onClose,
}: {
  canManage: boolean;
  onClose: () => void;
}) {
  const t = useT();
  const lists = useQuery(lprWatchlistsQuery());
  const [editing, setEditing] = useState<string | "new" | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<Watchlist | null>(null);

  return (
    <Modal title={t("plates.watchlistsTitle")} onClose={onClose} className="max-w-4xl">
      <div className="flex flex-col gap-4">
        <p className="text-sm text-on-surface-variant">{t("plates.watchlistsIntro")}</p>
        <ErrorNote error={lists.error} />

        {canManage && editing !== "new" && (
          <div>
            <Button variant="filled" onClick={() => setEditing("new")}>
              <Plus className="size-4" aria-hidden /> {t("plates.watchlistNew")}
            </Button>
          </div>
        )}

        {canManage && editing === "new" && (
          <WatchlistEditor onClose={() => setEditing(null)} />
        )}

        {lists.isSuccess && lists.data.length === 0 && (
          <Empty>{t("plates.watchlistEmpty")}</Empty>
        )}

        {lists.data && lists.data.length > 0 && (
          <ul className="flex flex-col gap-3" aria-label={t("plates.watchlists")}>
            {lists.data.map((w) => {
              const isExpanded = expanded === w.id;
              return (
                <li key={w.id}>
                  <Card>
                    <div className="flex flex-col gap-3">
                      <div className="flex flex-wrap items-start justify-between gap-3">
                        <div className="flex items-start gap-3">
                          <span
                            className="mt-1 size-3.5 shrink-0 rounded-full shadow-xs"
                            style={{ backgroundColor: w.color || "#ef4444" }}
                            aria-hidden
                          />
                          <div className="flex flex-col gap-0.5">
                            <span className="font-semibold text-ink flex items-center gap-2">
                              {w.name}
                              <span
                                className="inline-flex items-center gap-1 rounded-full px-2 py-0.2 text-[11px] font-medium text-white"
                                style={{ backgroundColor: w.color || "#ef4444" }}
                              >
                                <ShieldAlert className="size-3" aria-hidden />
                                {w.item_count}
                              </span>
                            </span>
                            {w.description && (
                              <span className="text-xs text-muted">{w.description}</span>
                            )}
                          </div>
                        </div>

                        <div className="flex items-center gap-1">
                          <Button
                            variant="tonal"
                            size="sm"
                            onClick={() => setExpanded(isExpanded ? null : w.id)}
                            aria-expanded={isExpanded}
                          >
                            {isExpanded ? (
                              <>
                                <ChevronUp className="size-4" aria-hidden />
                                {t("plates.watchlistPlates")}
                              </>
                            ) : (
                              <>
                                <ChevronDown className="size-4" aria-hidden />
                                {t("plates.watchlistPlates")} ({w.item_count})
                              </>
                            )}
                          </Button>
                          {canManage && (
                            <>
                              <IconButton
                                icon={Pencil}
                                size="sm"
                                variant="tonal"
                                aria-label={t("plates.watchlistEdit", { name: w.name })}
                                title={t("plates.watchlistEdit", { name: w.name })}
                                onClick={() => setEditing(editing === w.id ? null : w.id)}
                              />
                              <IconButton
                                icon={Trash2}
                                size="sm"
                                variant="tonal"
                                aria-label={t("plates.watchlistDelete", { name: w.name })}
                                title={t("plates.watchlistDelete", { name: w.name })}
                                onClick={() => setDeleting(w)}
                              />
                            </>
                          )}
                        </div>
                      </div>

                      {canManage && editing === w.id && (
                        <WatchlistEditor list={w} onClose={() => setEditing(null)} />
                      )}

                      {isExpanded && (
                        <WatchlistItemsList watchlistId={w.id} canManage={canManage} />
                      )}
                    </div>
                  </Card>
                </li>
              );
            })}
          </ul>
        )}

        {deleting && (
          <DeleteWatchlistDialog list={deleting} onClose={() => setDeleting(null)} />
        )}
      </div>
    </Modal>
  );
}

function WatchlistEditor({ list, onClose }: { list?: Watchlist; onClose: () => void }) {
  const t = useT();
  const qc = useQueryClient();
  const [name, setName] = useState(list?.name ?? "");
  const [description, setDescription] = useState(list?.description ?? "");
  const [color, setColor] = useState(list?.color ?? "#ef4444");

  const save = useMutation({
    mutationFn: async (e: FormEvent) => {
      e.preventDefault();
      if (list) {
        return unwrap(
          await api.PATCH("/api/v1/lpr/watchlists/{watchlistId}", {
            params: { path: { watchlistId: list.id } },
            body: { name: name.trim(), description: description.trim() || undefined, color },
          }),
        );
      }
      return unwrap(
        await api.POST("/api/v1/lpr/watchlists", {
          body: { name: name.trim(), description: description.trim() || undefined, color },
        }),
      );
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["lpr", "watchlists"] });
      onClose();
    },
  });

  return (
    <form onSubmit={(e) => void save.mutate(e)} className="flex flex-col gap-3 rounded-m3-md border border-outline-variant p-3 bg-surface-2">
      <Field label={t("plates.watchlistName")}>
        <TextInput value={name} onChange={(e) => setName(e.target.value)} required autoFocus />
      </Field>
      <Field label={t("plates.watchlistDesc")}>
        <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} />
      </Field>
      <Field label={t("plates.watchlistColor")}>
        <div className="flex flex-wrap items-center gap-2">
          {PRESET_COLORS.map((c) => (
            <button
              key={c.value}
              type="button"
              onClick={() => setColor(c.value)}
              className={`size-6 rounded-full border-2 transition-transform ${
                color === c.value ? "scale-125 border-primary ring-2 ring-primary/40" : "border-transparent hover:scale-110"
              }`}
              style={{ backgroundColor: c.value }}
              title={c.label}
              aria-label={c.label}
            />
          ))}
          <input
            type="color"
            value={color}
            onChange={(e) => setColor(e.target.value)}
            className="size-7 cursor-pointer rounded-full border-0 bg-transparent p-0"
            title="Color personalizado"
          />
        </div>
      </Field>
      <ErrorNote error={save.error} />
      <div className="flex justify-end gap-2">
        <Button type="button" variant="tonal" onClick={onClose}>
          {t("common.cancel")}
        </Button>
        <Button type="submit" variant="filled" disabled={save.isPending || !name.trim()}>
          {save.isPending ? t("common.saving") : t("common.accept")}
        </Button>
      </div>
    </form>
  );
}

function DeleteWatchlistDialog({ list, onClose }: { list: Watchlist; onClose: () => void }) {
  const t = useT();
  const qc = useQueryClient();
  const remove = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.DELETE("/api/v1/lpr/watchlists/{watchlistId}", {
          params: { path: { watchlistId: list.id } },
        }),
      ),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["lpr", "watchlists"] });
      onClose();
    },
  });

  return (
    <ConfirmDialog
      title={t("plates.watchlistDeleteTitle")}
      message={t("plates.watchlistDeleteMessage", { name: list.name })}
      confirmLabel={t("plates.watchlistDeleteTitle")}
      pending={remove.isPending}
      error={remove.error}
      onConfirm={() => remove.mutate()}
      onCancel={onClose}
    />
  );
}

function WatchlistItemsList({ watchlistId, canManage }: { watchlistId: string; canManage: boolean }) {
  const t = useT();
  const qc = useQueryClient();
  const items = useQuery(lprWatchlistItemsQuery(watchlistId));

  const [plate, setPlate] = useState("");
  const [brand, setBrand] = useState("");
  const [model, setModel] = useState("");
  const [color, setColor] = useState("");
  const [owner, setOwner] = useState("");
  const [notes, setNotes] = useState("");

  const add = useMutation({
    mutationFn: async (e: FormEvent) => {
      e.preventDefault();
      const normPlate = plate.trim().toUpperCase();
      if (!normPlate) return;
      return unwrap(
        await api.POST("/api/v1/lpr/watchlists/{watchlistId}/items", {
          params: { path: { watchlistId } },
          body: {
            plate: normPlate,
            vehicle_brand: brand.trim() || undefined,
            vehicle_model: model.trim() || undefined,
            vehicle_color: color.trim() || undefined,
            owner_name: owner.trim() || undefined,
            notes: notes.trim() || undefined,
          },
        }),
      );
    },
    onSuccess: async () => {
      setPlate("");
      setBrand("");
      setModel("");
      setColor("");
      setOwner("");
      setNotes("");
      await qc.invalidateQueries({ queryKey: ["lpr", "watchlists", watchlistId, "items"] });
      await qc.invalidateQueries({ queryKey: ["lpr", "watchlists"] });
    },
  });

  const remove = useMutation({
    mutationFn: async (itemId: string) =>
      unwrap(
        await api.DELETE("/api/v1/lpr/watchlists/{watchlistId}/items/{itemId}", {
          params: { path: { watchlistId, itemId } },
        }),
      ),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["lpr", "watchlists", watchlistId, "items"] });
      await qc.invalidateQueries({ queryKey: ["lpr", "watchlists"] });
    },
  });

  return (
    <div className="flex flex-col gap-3 rounded-m3-md border border-outline-variant bg-surface-2 p-3">
      <h4 className="text-sm font-bold text-ink">{t("plates.watchlistPlates")}</h4>
      <ErrorNote error={items.error} />

      {items.isSuccess && items.data.length === 0 && (
        <Empty>{t("plates.noPlatesInWatchlist")}</Empty>
      )}

      {items.data && items.data.length > 0 && (
        <ul className="flex flex-col gap-2">
          {items.data.map((item: WatchlistItem) => (
            <li
              key={item.id}
              className="flex flex-wrap items-center justify-between gap-2 rounded-m3-md bg-surface-1 p-2.5 shadow-xs"
            >
              <div className="flex flex-wrap items-center gap-3">
                <ArPlate plate={item.plate} />
                <div className="flex flex-col text-xs text-on-surface-variant">
                  {(item.vehicle_brand || item.vehicle_model || item.vehicle_color) && (
                    <span className="font-medium text-ink">
                      {[item.vehicle_brand, item.vehicle_model, item.vehicle_color].filter(Boolean).join(" · ")}
                    </span>
                  )}
                  {item.owner_name && (
                    <span>{item.owner_name}</span>
                  )}
                  {item.notes && (
                    <span className="italic text-muted">{item.notes}</span>
                  )}
                </div>
              </div>

              {canManage && (
                <IconButton
                  icon={Trash2}
                  size="sm"
                  variant="tonal"
                  aria-label={t("plates.removePlateConfirm", { plate: item.plate })}
                  title={t("plates.removePlateConfirm", { plate: item.plate })}
                  onClick={() => remove.mutate(item.id)}
                  disabled={remove.isPending}
                />
              )}
            </li>
          ))}
        </ul>
      )}

      {canManage && (
        <form onSubmit={(e) => void add.mutate(e)} className="mt-2 flex flex-col gap-2 border-t border-outline-variant pt-3">
          <span className="text-xs font-semibold uppercase tracking-wider text-muted">
            {t("plates.addPlate")}
          </span>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
            <TextInput
              placeholder={t("plates.plateInputPlaceholder")}
              value={plate}
              onChange={(e) => setPlate(e.target.value.toUpperCase())}
              required
              className="font-mono font-bold"
            />
            <TextInput
              placeholder={t("plates.brandPlaceholder")}
              value={brand}
              onChange={(e) => setBrand(e.target.value)}
            />
            <TextInput
              placeholder={t("plates.modelPlaceholder")}
              value={model}
              onChange={(e) => setModel(e.target.value)}
            />
            <TextInput
              placeholder={t("plates.colorPlaceholder")}
              value={color}
              onChange={(e) => setColor(e.target.value)}
            />
            <TextInput
              placeholder={t("plates.ownerPlaceholder")}
              value={owner}
              onChange={(e) => setOwner(e.target.value)}
            />
            <TextInput
              placeholder={t("plates.plateNotesPlaceholder")}
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
            />
          </div>
          <ErrorNote error={add.error} />
          <div className="flex justify-end">
            <Button type="submit" variant="filled" size="sm" disabled={add.isPending || !plate.trim()}>
              <Plus className="size-4" aria-hidden /> {t("plates.addPlate")}
            </Button>
          </div>
        </form>
      )}
    </div>
  );
}
