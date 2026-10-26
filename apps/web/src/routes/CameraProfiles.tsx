import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useState } from "react";
import { ApiError, api, type Schemas, unwrap } from "@/api/client";
import { cameraConfigProfileQuery, cameraConfigProfilesQuery, camerasQuery, meQuery } from "@/api/queries";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { ReapplyOutdated } from "@/components/camera/profile/ReapplyOutdated";
import { PatchFields } from "@/components/camera/profile/PatchFields";
import { draftErrors, draftToPatch, leafCount, type PatchDraft, patchToDraft, summarizePatch } from "@/components/camera/profile/patchForm";
import { Button, Empty, ErrorNote, Field, LinkButton, PageHeader, Pill, Table, TextInput, Textarea, Th } from "@/components/ui";
import { can } from "@/lib/perm";

type Profile = Schemas["CameraConfigProfile"];

/** Camera configuration profiles: named, tenant-wide sets of values that cameras inherit. */
export function CameraProfiles() {
  const me = useQuery(meQuery);
  const profiles = useQuery(cameraConfigProfilesQuery);
  const manage = can(me.data, "cameras.manage");
  const details = useQueries({ queries: (profiles.data ?? []).map((p) => cameraConfigProfileQuery(p.id)) });
  const outdated = new Map((profiles.data ?? []).map((p, i) => [p.id, details[i]?.data?.cameras.filter((c) => c.outdated).length ?? 0]));
  const [editing, setEditing] = useState<Profile | "new" | null>(null);
  const [deleting, setDeleting] = useState<Profile | null>(null);
  const [following, setFollowing] = useState<Profile | null>(null);

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader
        title="Perfiles de cámara"
        description="Conjuntos de valores con nombre que las cámaras heredan. Un cambio local de la cámara siempre gana sobre el perfil."
        actions={manage ? <Button variant="primary" onClick={() => setEditing("new")}><Icon icon={Plus} size="sm" /> Nuevo perfil</Button> : null}
      />
      <ErrorNote error={profiles.error} />
      {profiles.data?.length === 0 && <Empty>No hay perfiles de cámara.</Empty>}
      {!!profiles.data?.length && (
        <Table label="Perfiles de cámara">
          <thead>
            <tr><Th>Nombre</Th><Th>Valores</Th><Th>Cámaras</Th><Th /></tr>
          </thead>
          <tbody>
            {profiles.data.map((p) => (
              <tr key={p.id} className="border-t border-outline-variant align-top">
                <td>
                  <div className="font-medium">{p.name} <span className="text-xs font-normal text-muted">v{p.version}</span></div>
                  {p.description && <div className="text-xs text-muted">{p.description}</div>}
                </td>
                <td className="text-sm text-muted">{summarizePatch(p.patch)}</td>
                <td className="text-sm">
                  <div>{p.camera_count === 1 ? "1 cámara" : `${p.camera_count} cámaras`}</div>
                  {(outdated.get(p.id) ?? 0) > 0 && <Pill tone="warn" size="sm">{outdated.get(p.id)} desactualizadas</Pill>}
                </td>
                <td className="text-right">
                  {p.camera_count > 0 && <Button size="sm" className="mr-2" aria-label={`Ver cámaras de ${p.name}`} onClick={() => setFollowing(p)}>Cámaras</Button>}
                  {manage && <Button size="sm" className="mr-2" aria-label={`Editar ${p.name}`} onClick={() => setEditing(p)}>Editar</Button>}
                  {manage && <Button size="sm" variant="danger" aria-label={`Eliminar ${p.name}`} onClick={() => setDeleting(p)}>Eliminar</Button>}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {editing && <ProfileForm profile={editing === "new" ? undefined : editing} onClose={() => setEditing(null)} />}
      {deleting && <DeleteProfile profile={deleting} onClose={() => setDeleting(null)} />}
      {following && <FollowersDialog manage={manage} profile={following} onClose={() => setFollowing(null)} />}
    </div>
  );
}

function ProfileForm({ profile, onClose }: { profile?: Profile; onClose: () => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState(profile?.name ?? "");
  const [description, setDescription] = useState(profile?.description ?? "");
  const [draft, setDraft] = useState<PatchDraft>(() => patchToDraft(profile?.patch ?? {}));
  const invalid = Object.keys(draftErrors(draft)).length > 0;
  const patch = draftToPatch(draft);
  const save = useMutation({
    mutationFn: async () => {
      const body = { name: name.trim(), description: description.trim(), patch };
      if (profile) {
        return unwrap(await api.PUT("/api/v1/camera-config/profiles/{profileId}", { params: { path: { profileId: profile.id } }, body: { ...body, expected_version: profile.version } }));
      }
      return unwrap(await api.POST("/api/v1/camera-config/profiles", { body }));
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["camera-config", "profiles"] });
      onClose();
    },
  });
  const stale = save.error instanceof ApiError && save.error.status === 409 && !!profile;
  return (
    <Modal title={profile ? `Editar perfil: ${profile.name}` : "Nuevo perfil"} onClose={onClose}>
      <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); if (!invalid && name.trim()) save.mutate(); }}>
        <Field label="Nombre"><TextInput value={name} maxLength={120} onChange={(e) => setName(e.target.value)} required /></Field>
        <Field label="Descripción"><Textarea value={description} maxLength={1000} onChange={(e) => setDescription(e.target.value)} /></Field>
        <p className="text-xs text-muted">Lo que dejes en «No cambiar» queda fuera del perfil. Editar un perfil no modifica las cámaras: quedan como desactualizadas hasta que lo vuelvas a aplicar.</p>
        <PatchFields draft={draft} onChange={setDraft} />
        <p className="text-xs text-muted" data-testid="profile-leaf-count">{leafCount(patch) === 1 ? "1 valor definido." : `${leafCount(patch)} valores definidos.`}</p>
        {stale ? <p role="alert" className="rounded-m3-lg bg-warn/10 px-4 py-3 text-sm text-warn">El perfil cambió mientras lo editabas. Cerrá y volvé a abrirlo.</p> : <ErrorNote error={save.error} />}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="text" onClick={onClose}>Cancelar</Button>
          <Button type="submit" variant="primary" disabled={save.isPending || invalid || !name.trim() || leafCount(patch) === 0}>{save.isPending ? "Guardando…" : "Guardar"}</Button>
        </div>
      </form>
    </Modal>
  );
}

function DeleteProfile({ profile, onClose }: { profile: Profile; onClose: () => void }) {
  const qc = useQueryClient();
  const del = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/api/v1/camera-config/profiles/{profileId}", { params: { path: { profileId: profile.id } } })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["camera-config", "profiles"] });
      onClose();
    },
  });
  const inUse = del.error instanceof ApiError && del.error.status === 409;
  return (
    <ConfirmDialog
      title="Eliminar perfil"
      message={`¿Eliminar el perfil «${profile.name}»?`}
      confirmLabel="Eliminar"
      pending={del.isPending}
      error={inUse ? new Error(`${profile.camera_count === 1 ? "1 cámara usa" : `${profile.camera_count} cámaras usan`} este perfil. Desvinculalas antes de eliminarlo.`) : del.error}
      onConfirm={() => del.mutate()}
      onCancel={onClose}
    />
  );
}

function FollowersDialog({ profile, onClose, manage }: { profile: Profile; onClose: () => void; manage?: boolean }) {
  const detail = useQuery(cameraConfigProfileQuery(profile.id));
  const cameras = useQuery(camerasQuery({}));
  const names = new Map(cameras.data?.map((c) => [c.id, c.display_name]));
  const old = detail.data?.cameras.filter((c) => c.outdated) ?? [];
  return (
    <Modal title={`Cámaras de ${profile.name}`} onClose={onClose} className="max-w-xl">
      <ErrorNote error={detail.error} />
      {detail.data && old.length > 0 && (
        <section aria-label="Cámaras desactualizadas" className="flex flex-col gap-2">
          <h3 className="text-sm font-semibold">Reaplicar a las cámaras desactualizadas</h3>
          <p className="text-xs text-muted">Abrí cada cámara y en «Perfil aplicado» elegí este perfil para revisar y aplicar los cambios.</p>
          {manage && <ReapplyOutdated profileId={profile.id} cameras={old.map((c) => ({ id: c.camera_id, name: names.get(c.camera_id) ?? c.camera_id }))} />}
          <ul className="flex flex-col gap-1">
            {old.map((c) => (
              <li key={c.camera_id} className="flex items-center justify-between gap-2 text-sm">
                <span>{names.get(c.camera_id) ?? c.camera_id}</span>
                <LinkButton size="sm" to="/cameras/$cameraId/frigate" params={{ cameraId: c.camera_id }}>Abrir cámara</LinkButton>
              </li>
            ))}
          </ul>
        </section>
      )}
      {detail.data && (
        <section aria-label="Cámaras al día" className="flex flex-col gap-1">
          <h3 className="text-sm font-semibold">Al día</h3>
          <ul className="text-sm">
            {detail.data.cameras.filter((c) => !c.outdated).map((c) => <li key={c.camera_id}>{names.get(c.camera_id) ?? c.camera_id}</li>)}
          </ul>
        </section>
      )}
      <div className="flex justify-end"><Button variant="text" onClick={onClose}>Cerrar</Button></div>
    </Modal>
  );
}
