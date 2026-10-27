import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, type Schemas } from "@/api/client";
import { applyCameraFrigateYaml, cameraConfigQuery, cameraFrigateYamlQuery, validateCameraFrigateYaml } from "@/api/queries";
import { Button } from "@/components/ui";
import { SyncStatusBadge } from "../SyncStatusBadge";
import { fmtValue } from "./rollback";

type Validation = Schemas["CameraYamlValidation"];
const KIND: Record<string, string> = { added: "Agregado", removed: "Quitado", changed: "Cambiado" };

/** Edits the YAML of one top-level Frigate section: validate, review the diff, confirm managed parameters, save. */
export function YamlSectionEditor({ cameraId, section, canEdit, readOnlyReason }: { cameraId: string; section: string; canEdit: boolean; readOnlyReason?: string }) {
  const qc = useQueryClient();
  const source = useQuery({ ...cameraFrigateYamlQuery(cameraId, section), enabled: canEdit });
  const config = useQuery(cameraConfigQuery(cameraId));
  const [text, setText] = useState<string | null>(null);
  const [checked, setChecked] = useState<{ text: string; result: Validation } | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  const original = source.data?.yaml ?? "";
  const current = text ?? original;
  const validation = checked && checked.text === current ? checked.result : null;
  const conflicts = validation?.managed_conflicts ?? [];
  const managed = (validation?.valid && conflicts.length > 0) || false;

  const validate = useMutation({
    mutationFn: () => validateCameraFrigateYaml(cameraId, { section, yaml: current }),
    onSuccess: (result) => {
      setChecked({ text: current, result });
      setConfirmed(false);
      setNotice(null);
    },
  });
  const apply = useMutation({
    mutationFn: (confirm: boolean) => applyCameraFrigateYaml(cameraId, { section, yaml: current, expected_revision: validation?.revision ?? source.data?.revision ?? 0, confirm_managed_conflicts: confirm }),
    onSuccess: async (res) => {
      setNotice(res.marked_drift
        ? "Se guardó. Cambiaste parámetros que administra OpenVMS: la cámara quedó con diferencia detectada."
        : "YAML guardado.");
      setText(null);
      setChecked(null);
      setConfirmed(false);
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["cameras", cameraId, "frigate-yaml"] }),
        qc.invalidateQueries({ queryKey: ["cameras", cameraId, "config-model"] }),
        qc.invalidateQueries({ queryKey: ["cameras", cameraId, "config-history"] }),
        qc.invalidateQueries({ queryKey: ["cameras", cameraId, "advanced"] }),
      ]);
    },
    onError: (err) => {
      // The server wants the explicit confirmation: validate again so the conflicts and the checkbox show up.
      if (err instanceof ApiError && err.status === 409 && err.code === "managed_conflict") validate.mutate();
    },
  });

  if (!canEdit) {
    return <p role="status" className="rounded-m3-lg bg-surface-2 px-4 py-3 text-sm text-muted">{readOnlyReason ?? "Requiere el permiso cameras.config.yaml."}</p>;
  }
  if (source.isLoading) return <p role="status" className="text-sm text-muted">Cargando YAML…</p>;
  if (source.isError) return <p role="alert" className="text-sm text-bad">No se pudo cargar el YAML de la sección.</p>;

  const unchanged = current === original;
  const hasChanges = (validation?.changes.length ?? 0) > 0;
  const canSave = !!validation?.valid && hasChanges && (!managed || confirmed) && !apply.isPending;
  const err = apply.error;
  const revisionConflict = err instanceof ApiError && err.status === 409 && err.code === "revision_conflict";
  const managedConflictErr = err instanceof ApiError && err.status === 409 && err.code === "managed_conflict";

  return (
    <div className="flex flex-col gap-3">
      <label className="flex flex-col gap-1 text-sm font-semibold">
        {`YAML de ${section}`}
        <textarea
          aria-label={`YAML de ${section}`}
          aria-invalid={validation ? !validation.valid : undefined}
          spellCheck={false}
          rows={Math.min(24, Math.max(8, current.split("\n").length + 1))}
          className="w-full rounded-m3-lg border border-outline-variant bg-surface-1 p-3 font-mono text-xs"
          value={current}
          onChange={(e) => { setText(e.target.value); setNotice(null); }}
        />
      </label>
      {!source.data?.secrets_visible && <p className="text-xs text-muted">Las credenciales están ocultas; los valores ocultos que no toques se conservan.</p>}

      <div className="flex flex-wrap gap-2">
        <Button variant="secondary" disabled={validate.isPending} onClick={() => validate.mutate()}>{validate.isPending ? "Validando…" : "Validar"}</Button>
        <Button variant="primary" disabled={!canSave} onClick={() => apply.mutate(managed && confirmed)}>{apply.isPending ? "Guardando…" : "Guardar"}</Button>
        <Button variant="text" disabled={unchanged} onClick={() => { setText(null); setChecked(null); setNotice(null); }}>Descartar cambios</Button>
      </div>

      {validate.isError && <p role="alert" className="text-sm text-bad">No se pudo validar el YAML.</p>}
      {validation && !validation.valid && (
        <div role="alert" className="rounded-m3-lg bg-bad/12 px-4 py-3 text-sm text-bad">
          <p>El YAML no es válido.</p>
          <ul aria-label="Errores" className="list-disc pl-5">
            {validation.syntax.map((s, i) => <li key={`s${i}`}>Línea {s.line}, columna {s.column}: {s.message}</li>)}
            {validation.schema.map((s, i) => <li key={`e${i}`}>{s.path ? `${s.path}: ` : ""}{s.message}</li>)}
          </ul>
        </div>
      )}
      {validation && validation.warnings.length > 0 && (
        <ul aria-label="Advertencias" className="list-disc rounded-m3-lg bg-warn/10 px-4 py-3 pl-8 text-sm text-warn">
          {validation.warnings.map((w) => <li key={w}>{w}</li>)}
        </ul>
      )}
      {validation?.valid && (
        <section aria-label="Diferencias" className="flex flex-col gap-1">
          <h4 className="text-sm font-bold">Diferencias</h4>
          {hasChanges ? (
            <ul className="list-disc pl-5 text-xs">
              {validation.changes.map((c) => <li key={c.path}>{KIND[c.kind] ?? c.kind} {c.path}: {fmtValue(c.from)} → {fmtValue(c.to)}</li>)}
            </ul>
          ) : <p className="text-xs text-muted">No hay cambios respecto de la configuración actual.</p>}
        </section>
      )}
      {managed && (
        <div role="alert" className="flex flex-col gap-2 rounded-m3-lg bg-warn/10 px-4 py-3 text-sm text-warn">
          <p>Estos parámetros los administra OpenVMS: {conflicts.map((c) => c.patch_path).join(", ")}.</p>
          <ul className="list-disc pl-5 text-xs">
            {conflicts.map((c) => <li key={c.patch_path}>{c.patch_path}: OpenVMS {fmtValue(c.desired)} · YAML {fmtValue(c.new)}</li>)}
          </ul>
          <label className="flex items-center gap-2">
            <input type="checkbox" checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} />
            Entiendo que la cámara quedará con diferencia detectada
          </label>
        </div>
      )}

      {revisionConflict && (
        <div role="alert" className="rounded-m3-lg bg-warn/10 px-4 py-3 text-sm text-warn">
          <p>La configuración cambió mientras editabas. Recargá el YAML y volvé a aplicar tus cambios.</p>
          <Button variant="secondary" onClick={() => { apply.reset(); setText(null); setChecked(null); void source.refetch(); }}>Recargar YAML</Button>
        </div>
      )}
      {managedConflictErr && <p role="alert" className="text-sm text-warn">Confirmá los parámetros administrados por OpenVMS para guardar.</p>}
      {err && !revisionConflict && !managedConflictErr && <p role="alert" className="text-sm text-bad">No se pudo guardar el YAML. {err.message}</p>}
      {notice && (
        <p role="status" className="flex items-center gap-2 text-sm text-ok">
          {notice}
          {config.data && <SyncStatusBadge state={config.data.state.sync_state} />}
        </p>
      )}
    </div>
  );
}
