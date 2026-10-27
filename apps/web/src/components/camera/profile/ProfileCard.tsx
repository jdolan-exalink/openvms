import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { cameraConfigProfilesQuery, cameraConfigQuery, meQuery } from "@/api/queries";
import { Modal } from "@/components/Modal";
import { Button, ErrorNote, Pill, Select } from "@/components/ui";
import { can } from "@/lib/perm";

type Review = Schemas["CameraConfigProfileReview"];

const fmt = (v: unknown): string => (v === undefined || v === null ? "—" : typeof v === "boolean" ? (v ? "ON" : "OFF") : Array.isArray(v) ? v.join(", ") : typeof v === "object" ? JSON.stringify(v) : String(v));

/** Review (and then apply) the assignment of a profile, or its removal when profileId is null. */
function useProfileChange(cameraId: string) {
  const qc = useQueryClient();
  const review = useMutation({
    mutationFn: async (v: { profileId: string | null; keep: boolean }): Promise<Review> =>
      unwrap(await api.POST("/api/v1/cameras/{cameraId}/profile/review", { params: { path: { cameraId } }, body: { profile_id: v.profileId, keep_values: v.keep } })),
  });
  const apply = useMutation({
    mutationFn: async (v: { profileId: string | null; keep: boolean }) =>
      unwrap(await api.POST("/api/v1/cameras/{cameraId}/profile/apply", { params: { path: { cameraId } }, body: { profile_id: v.profileId, keep_values: v.keep } })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["cameras", cameraId] });
      await qc.invalidateQueries({ queryKey: ["camera-config"] });
    },
  });
  return { review, apply };
}

function ReviewBody({ r }: { r: Review }) {
  return (
    <>
      <p className="text-sm font-semibold" data-testid="profile-param-count">
        {r.parameters === 0 ? "No hay parámetros para modificar." : r.parameters === 1 ? "Se modificará 1 parámetro." : `Se modificarán ${r.parameters} parámetros.`}
      </p>
      {r.review.changes.length > 0 && (
        <ul className="list-disc pl-5 text-sm">
          {r.review.changes.map((c) => <li key={c.path}>{c.label}: {fmt(c.from)} → {fmt(c.to)}</li>)}
        </ul>
      )}
      {r.review.impact.length > 0 && (
        <ul className="list-disc pl-5 text-xs text-muted">{r.review.impact.map((i) => <li key={i.code}>{i.message}</li>)}</ul>
      )}
      {r.shadowed.length > 0 && (
        <p role="note" className="rounded-m3-lg bg-warn/10 px-4 py-3 text-sm text-warn">
          Estos valores del perfil no se aplican porque la cámara tiene cambios locales: {r.shadowed.join(", ")}.
        </p>
      )}
    </>
  );
}

function ChangeProfileDialog({ cameraId, currentId, onClose }: { cameraId: string; currentId?: string; onClose: () => void }) {
  const profiles = useQuery(cameraConfigProfilesQuery);
  const { review, apply } = useProfileChange(cameraId);
  const [pick, setPick] = useState("");
  const choose = (id: string) => {
    setPick(id);
    apply.reset();
    if (id) review.mutate({ profileId: id, keep: true });
    else review.reset();
  };
  const done = apply.isSuccess;
  return (
    <Modal title="Cambiar perfil" onClose={onClose} className="max-w-xl">
      <ErrorNote error={profiles.error} />
      <label className="flex flex-col gap-1 text-sm">
        <span className="font-medium">Perfil</span>
        <Select value={pick} onChange={(e) => choose(e.target.value)} disabled={done}>
          <option value="">Elegí un perfil</option>
          {profiles.data?.map((p) => <option key={p.id} value={p.id}>{p.name}{p.id === currentId ? " (actual)" : ""}</option>)}
        </Select>
      </label>
      {review.isPending && <p role="status" className="text-sm text-muted">Calculando cambios…</p>}
      {review.data && pick && <ReviewBody r={review.data} />}
      <ErrorNote error={review.error ?? apply.error} />
      {done && <p role="status" className="text-sm text-ok">Perfil aplicado.</p>}
      <div className="flex justify-end gap-2">
        <Button variant="text" onClick={onClose}>{done ? "Cerrar" : "Cancelar"}</Button>
        {!done && (
          <Button variant="primary" disabled={!pick || !review.data || apply.isPending} onClick={() => apply.mutate({ profileId: pick, keep: true })}>
            {apply.isPending ? "Aplicando…" : "Aplicar"}
          </Button>
        )}
      </div>
    </Modal>
  );
}

function UnlinkDialog({ cameraId, name, onClose }: { cameraId: string; name: string; onClose: () => void }) {
  const { review, apply } = useProfileChange(cameraId);
  const [keep, setKeep] = useState(true);
  const { mutate } = review;
  useEffect(() => mutate({ profileId: null, keep }), [mutate, keep]);
  const done = apply.isSuccess;
  return (
    <Modal title="Desvincular perfil" onClose={onClose} className="max-w-xl">
      <p className="text-sm">La cámara dejará de seguir el perfil «{name}». La configuración del motor no se toca.</p>
      <fieldset className="flex flex-col gap-1" disabled={done}>
        <legend className="text-sm font-medium">¿Qué pasa con los valores del perfil?</legend>
        <label className="flex min-h-11 items-center gap-3 text-sm">
          <input type="radio" name="keep" className="size-5 accent-primary" checked={keep} onChange={() => setKeep(true)} />
          Conservar los valores actuales como cambios locales
        </label>
        <label className="flex min-h-11 items-center gap-3 text-sm">
          <input type="radio" name="keep" className="size-5 accent-primary" checked={!keep} onChange={() => setKeep(false)} />
          Descartar los valores del perfil
        </label>
      </fieldset>
      {review.data && (
        <p className="text-sm text-muted" data-testid="unlink-summary">
          {keep ? `Se conservarán ${review.data.converted.length} valores como cambios locales.` : `Se descartarán ${review.data.dropped.length} valores del perfil.`}
        </p>
      )}
      <ErrorNote error={review.error ?? apply.error} />
      {done && <p role="status" className="text-sm text-ok">Perfil desvinculado.</p>}
      <div className="flex justify-end gap-2">
        <Button variant="text" onClick={onClose}>{done ? "Cerrar" : "Cancelar"}</Button>
        {!done && <Button variant="danger" disabled={apply.isPending} onClick={() => apply.mutate({ profileId: null, keep })}>{apply.isPending ? "Desvinculando…" : "Desvincular"}</Button>}
      </div>
    </Modal>
  );
}

/** "Perfil aplicado" of the General tab: which profile the camera follows, and how to change, unlink or audit it. */
export function ProfileCard({ cameraId }: { cameraId: string }) {
  const me = useQuery(meQuery);
  const config = useQuery(cameraConfigQuery(cameraId));
  const [dialog, setDialog] = useState<"change" | "unlink" | null>(null);
  const [showOverrides, setShowOverrides] = useState(false);
  const manage = can(me.data, "cameras.manage");
  if (config.isLoading) return null;
  const profile = config.data?.profile;
  const overrides = Object.entries(config.data?.provenance ?? {}).filter(([, v]) => v.source === "override").map(([p]) => p).sort();

  return (
    <section aria-label="Perfil aplicado" className="flex flex-col gap-3 rounded-m3-lg border border-outline-variant/50 bg-surface-2 p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h3 className="text-sm font-semibold">Perfil aplicado</h3>
          {profile ? (
            <p className="flex flex-wrap items-center gap-2 text-sm">
              <span className="font-medium">{profile.name}</span>
              <span className="text-xs text-muted">versión {profile.version}</span>
              {profile.outdated && <Pill tone="warn" size="sm">Desactualizado</Pill>}
            </p>
          ) : (
            <p className="text-sm text-muted">Sin perfil: la cámara usa los valores del motor y sus cambios locales.</p>
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          {manage && <Button variant="secondary" onClick={() => setDialog("change")}>Cambiar perfil</Button>}
          {manage && profile && <Button variant="outlined" onClick={() => setDialog("unlink")}>Desvincular perfil</Button>}
          <Button variant="text" aria-expanded={showOverrides} onClick={() => setShowOverrides((s) => !s)}>Ver overrides</Button>
        </div>
      </div>
      {profile?.outdated && <p className="text-xs text-warn">El perfil cambió desde la última vez que se aplicó a esta cámara. Volvé a aplicarlo para actualizarla.</p>}
      {showOverrides && (
        overrides.length === 0 ? (
          <p className="text-sm text-muted">Esta cámara no tiene valores modificados localmente.</p>
        ) : (
          <ul aria-label="Overrides" className="list-disc pl-5 font-mono text-xs">{overrides.map((p) => <li key={p}>{p}</li>)}</ul>
        )
      )}
      {dialog === "change" && <ChangeProfileDialog cameraId={cameraId} currentId={profile?.id} onClose={() => setDialog(null)} />}
      {dialog === "unlink" && profile && <UnlinkDialog cameraId={cameraId} name={profile.name} onClose={() => setDialog(null)} />}
    </section>
  );
}
