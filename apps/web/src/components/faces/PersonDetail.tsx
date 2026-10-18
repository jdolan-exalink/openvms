import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ImagePlus, RefreshCw, Trash2 } from "lucide-react";
import { type FormEvent, useRef, useState } from "react";
import { api, postForm, type Schemas, unwrap } from "@/api/client";
import { faceImagesQuery, faceImageUrl, faceSyncQuery } from "@/api/queries";
import { SyncBadge } from "@/components/faces/SyncBadge";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Button, Card, ErrorNote, Field, IconButton, TextInput, Textarea } from "@/components/ui";
import { useT } from "@/i18n";
import { fmtDateTime } from "@/lib/format";

/**
 * PersonDetail is one person: editable name and notes, the enrollment photo gallery, and the
 * per-server sync state. Without faces.manage everything is read-only.
 */
export function PersonDetail({ person, canManage, tenantId, onBack }: { person: Schemas["FacePerson"]; canManage: boolean; tenantId?: string; onBack: () => void }) {
  const t = useT();
  const qc = useQueryClient();
  const images = useQuery(faceImagesQuery(person.id, tenantId));
  const syncStatus = useQuery(faceSyncQuery(person.id, tenantId));
  const [name, setName] = useState(person.display_name);
  const [notes, setNotes] = useState(person.notes);
  const [confirmPerson, setConfirmPerson] = useState(false);
  const [confirmImage, setConfirmImage] = useState<{ id: string; n: number } | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const refresh = () => qc.invalidateQueries({ queryKey: ["faces"] });
  const path = { params: { path: { personId: person.id }, query: { tenant_id: tenantId } } };

  const save = useMutation({
    mutationFn: async (e: FormEvent) => {
      e.preventDefault();
      return unwrap(await api.PATCH("/api/v1/faces/persons/{personId}", { ...path, body: { display_name: name.trim(), notes: notes.trim() } }));
    },
    onSuccess: refresh,
  });
  const upload = useMutation({
    mutationFn: async (file: File) => {
      const form = new FormData();
      form.append("file", file);
      return postForm<Schemas["FaceImage"]>(`/api/v1/faces/persons/${person.id}/images${tenantId ? `?tenant_id=${encodeURIComponent(tenantId)}` : ""}`, form);
    },
    onSuccess: refresh,
  });
  const removeImage = useMutation({
    mutationFn: async (imageId: string) =>
      unwrap(await api.DELETE("/api/v1/faces/persons/{personId}/images/{imageId}", { params: { path: { personId: person.id, imageId }, query: { tenant_id: tenantId } } })),
    onSuccess: () => {
      setConfirmImage(null);
      return refresh();
    },
  });
  const removePerson = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/api/v1/faces/persons/{personId}", path)),
    onSuccess: async () => {
      await refresh();
      onBack();
    },
  });
  const retry = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/faces/persons/{personId}/sync/retry", path)),
    onSuccess: refresh,
  });

  const dirty = name.trim() !== person.display_name || notes.trim() !== person.notes;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Button variant="text" onClick={onBack}>
          <ArrowLeft className="size-4" aria-hidden /> {t("faces.back")}
        </Button>
        {canManage && (
          <Button variant="danger" onClick={() => setConfirmPerson(true)}>
            <Trash2 className="size-4" aria-hidden /> {t("faces.deletePerson")}
          </Button>
        )}
      </div>
      <h2 className="text-2xl font-bold">{person.display_name}</h2>
      <p className="font-mono text-xs text-muted">{t("faces.frigateName", { name: person.frigate_name })}</p>

      {canManage && (
        <form className="grid gap-3 rounded-m3-xl bg-surface-1 p-4 sm:grid-cols-2" onSubmit={(e) => save.mutate(e)}>
          <Field label={t("faces.personName")}>
            <TextInput value={name} maxLength={120} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t("faces.personNotes")}>
            <Textarea value={notes} maxLength={2000} rows={2} onChange={(e) => setNotes(e.target.value)} />
          </Field>
          <div className="sm:col-span-2">
            <Button type="submit" variant="filled" disabled={!dirty || !name.trim() || save.isPending}>
              {save.isPending ? t("faces.saving") : t("faces.save")}
            </Button>
          </div>
          <div className="sm:col-span-2">
            <ErrorNote error={save.error} />
          </div>
        </form>
      )}
      {!canManage && person.notes && <p className="text-sm text-on-surface-variant">{person.notes}</p>}

      <Card title={t("faces.gallery")}>
        <ErrorNote error={images.error} />
        {images.isSuccess && images.data.length === 0 && <p className="text-sm text-on-surface-variant">{t("faces.galleryEmpty")}</p>}
        {images.data && images.data.length > 0 && (
          <ul className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-6" aria-label={t("faces.gallery")}>
            {images.data.map((img, i) => (
              <li key={img.id} className="relative overflow-hidden rounded-m3-lg bg-surface-2">
                <img src={faceImageUrl(person.id, img.id, tenantId)} alt={t("faces.imageAlt", { n: i + 1, name: person.display_name })} loading="lazy" className="aspect-square w-full object-cover" />
                {canManage && (
                  <span className="absolute top-1 right-1">
                    <IconButton icon={Trash2} size="sm" variant="tonal" aria-label={t("faces.deleteImage", { n: i + 1 })} title={t("faces.deleteImage", { n: i + 1 })} onClick={() => setConfirmImage({ id: img.id, n: i + 1 })} />
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
        {canManage && (
          <div className="flex flex-col gap-2">
            <label className="inline-flex w-fit cursor-pointer items-center gap-2 rounded-full bg-secondary-container px-4 py-2.5 text-sm font-medium text-on-secondary-container">
              <ImagePlus className="size-4" aria-hidden />
              {upload.isPending ? t("faces.uploading") : t("faces.upload")}
              <input
                ref={fileInput}
                type="file"
                accept="image/jpeg,image/png,image/webp"
                className="sr-only"
                aria-label={t("faces.upload")}
                disabled={upload.isPending}
                onChange={(e) => {
                  const file = e.target.files?.[0];
                  if (file) upload.mutate(file);
                  if (fileInput.current) fileInput.current.value = "";
                }}
              />
            </label>
            <p className="text-xs text-muted">{t("faces.uploadHint")}</p>
            <ErrorNote error={upload.error} />
          </div>
        )}
      </Card>

      <Card title={t("faces.syncTitle")}>
        <ErrorNote error={syncStatus.error} />
        {syncStatus.isSuccess && syncStatus.data.length === 0 && <p className="text-sm text-on-surface-variant">{t("faces.syncEmpty")}</p>}
        {syncStatus.data && syncStatus.data.length > 0 && (
          <ul className="flex flex-col gap-2" aria-label={t("faces.syncTitle")}>
            {syncStatus.data.map((s) => (
              <li key={s.server_id} className="flex flex-wrap items-center justify-between gap-2 text-sm">
                <span className="font-medium">{s.server_name}</span>
                <span className="flex items-center gap-3">
                  {s.last_error && <span className="max-w-xs truncate text-xs text-muted" title={s.last_error}>{s.last_error}</span>}
                  <span className="text-xs text-muted">{fmtDateTime(s.updated_at)}</span>
                  <SyncBadge status={s.status} />
                </span>
              </li>
            ))}
          </ul>
        )}
        {canManage && (
          <div className="flex flex-col gap-2">
            <Button variant="tonal" className="w-fit" disabled={retry.isPending} onClick={() => retry.mutate()}>
              <RefreshCw className="size-4" aria-hidden /> {retry.isPending ? t("faces.retrying") : t("faces.retry")}
            </Button>
            <ErrorNote error={retry.error} />
          </div>
        )}
      </Card>

      {confirmPerson && (
        <ConfirmDialog
          title={t("faces.deletePersonTitle", { name: person.display_name })}
          message={t("faces.deletePersonMessage")}
          confirmLabel={t("faces.deletePerson")}
          pending={removePerson.isPending}
          error={removePerson.error}
          onConfirm={() => removePerson.mutate()}
          onCancel={() => setConfirmPerson(false)}
        />
      )}
      {confirmImage && (
        <ConfirmDialog
          title={t("faces.deleteImageTitle")}
          message={t("faces.deleteImageMessage")}
          confirmLabel={t("faces.deleteImageConfirm")}
          pending={removeImage.isPending}
          error={removeImage.error}
          onConfirm={() => removeImage.mutate(confirmImage.id)}
          onCancel={() => setConfirmImage(null)}
        />
      )}
    </div>
  );
}
