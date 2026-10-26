import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap, type Schemas } from "@/api/client";
import { cameraPtzPresetsQuery, cameraPtzToursQuery } from "@/api/queries";
import { Button, ErrorNote, Field, Select, TextInput } from "@/components/ui";

type Step = Schemas["CameraPtzTourStep"];
type Draft = { id?: string; name: string; repeat: number; steps: Step[] };

export const TOUR_RUN_NOTE = "Los tours se guardan en OpenVMS; la ejecución automática todavía no está disponible. Mientras tanto se recorren manualmente con «Ir» en cada preset.";

const emptyDraft = (): Draft => ({ name: "", repeat: 0, steps: [] });

/** Stored tours: sequence of presets with dwell time and speed, repeat count. CRUD only; no runner. */
export function ToursPanel({ cameraId, canManage }: { cameraId: string; canManage: boolean }) {
  const qc = useQueryClient();
  const tours = useQuery(cameraPtzToursQuery(cameraId));
  const presets = useQuery({ ...cameraPtzPresetsQuery(cameraId), enabled: canManage });
  const [draft, setDraft] = useState<Draft | null>(null);
  const refresh = () => qc.invalidateQueries({ queryKey: ["cameras", cameraId, "ptz-tours"] });

  const save = useMutation({
    mutationFn: async (d: Draft) => {
      const body = { name: d.name.trim(), repeat_count: d.repeat, steps: d.steps };
      return d.id
        ? unwrap(await api.PUT("/api/v1/cameras/{cameraId}/ptz/tours/{tourId}", { params: { path: { cameraId, tourId: d.id } }, body }))
        : unwrap(await api.POST("/api/v1/cameras/{cameraId}/ptz/tours", { params: { path: { cameraId } }, body }));
    },
    onSuccess: () => {
      setDraft(null);
      void refresh();
    },
  });
  const remove = useMutation({
    mutationFn: async (tourId: string) => unwrap(await api.DELETE("/api/v1/cameras/{cameraId}/ptz/tours/{tourId}", { params: { path: { cameraId, tourId } } })),
    onSuccess: () => void refresh(),
  });

  const available = presets.data?.presets ?? [];
  const setStep = (i: number, patch: Partial<Step>) => setDraft((d) => d && { ...d, steps: d.steps.map((s, j) => (j === i ? { ...s, ...patch } : s)) });
  const valid = !!draft && draft.name.trim() !== "" && draft.steps.length > 0 && draft.steps.every((s) => s.preset_token !== "");

  return (
    <section aria-label="Tours" className="flex flex-col gap-3 rounded-m3-lg border border-outline-variant/50 bg-surface-2 p-4">
      <h3 className="text-sm font-semibold">Tours</h3>
      <p className="text-xs text-muted">{TOUR_RUN_NOTE}</p>
      {tours.isError && <ErrorNote error={tours.error} />}
      {tours.isSuccess && tours.data.tours.length === 0 && !draft && <p className="text-xs text-muted">Esta cámara no tiene tours guardados.</p>}
      <ul className="flex flex-col gap-1.5">
        {tours.data?.tours.map((t) => (
          <li key={t.id} className="flex items-center justify-between gap-2 text-sm">
            <span>
              {t.name} <span className="text-xs text-muted">· {t.steps.length} pasos · {t.repeat_count === 0 ? "repite sin fin" : `${t.repeat_count} repeticiones`}</span>
            </span>
            {canManage && (
              <span className="flex gap-1.5">
                <Button size="sm" onClick={() => setDraft({ id: t.id, name: t.name, repeat: t.repeat_count, steps: t.steps })} aria-label={`Editar ${t.name}`}>Editar</Button>
                <Button size="sm" disabled={remove.isPending} onClick={() => remove.mutate(t.id)} aria-label={`Eliminar ${t.name}`}>Eliminar</Button>
              </span>
            )}
          </li>
        ))}
      </ul>
      <ErrorNote error={remove.error} />

      {canManage && !draft && (
        <div>
          <Button size="sm" onClick={() => setDraft(emptyDraft())}>Nuevo tour</Button>
        </div>
      )}
      {!canManage && <p className="text-xs text-muted">Se necesita el permiso de gestión de cámaras para crear o editar tours.</p>}

      {draft && (
        <form
          aria-label="Editor de tour"
          className="flex flex-col gap-3 rounded-m3-md border border-outline-variant/50 p-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (valid) save.mutate(draft);
          }}
        >
          <Field label="Nombre del tour">
            <TextInput value={draft.name} maxLength={128} onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
          </Field>
          <Field label="Repeticiones" hint="0 repite hasta que se detenga.">
            <TextInput type="number" min={0} max={1000} value={draft.repeat} onChange={(e) => setDraft({ ...draft, repeat: Math.max(0, Math.min(1000, Number(e.target.value) || 0)) })} />
          </Field>
          {draft.steps.map((s, i) => (
            <div key={i} role="group" aria-label={`Paso ${i + 1}`} className="grid grid-cols-1 gap-2 sm:grid-cols-4">
              <Field label="Preset">
                <Select value={s.preset_token} onChange={(e) => setStep(i, { preset_token: e.target.value, preset_name: available.find((p) => p.token === e.target.value)?.name })}>
                  <option value="">Elegir…</option>
                  {s.preset_token && !available.some((p) => p.token === s.preset_token) && <option value={s.preset_token}>{s.preset_name || `Preset ${s.preset_token}`}</option>}
                  {available.map((p) => <option key={p.token} value={p.token}>{p.name || `Preset ${p.token}`}</option>)}
                </Select>
              </Field>
              <Field label="Permanencia (s)">
                <TextInput type="number" min={0} max={3600} value={s.dwell_seconds} onChange={(e) => setStep(i, { dwell_seconds: Math.max(0, Math.min(3600, Number(e.target.value) || 0)) })} />
              </Field>
              <Field label="Velocidad">
                <TextInput type="number" min={0} max={1} step={0.1} value={s.speed} onChange={(e) => setStep(i, { speed: Math.max(0, Math.min(1, Number(e.target.value) || 0)) })} />
              </Field>
              <div className="flex items-end">
                <Button size="sm" onClick={() => setDraft({ ...draft, steps: draft.steps.filter((_, j) => j !== i) })}>Quitar paso</Button>
              </div>
            </div>
          ))}
          <div className="flex flex-wrap gap-2">
            <Button size="sm" disabled={draft.steps.length >= 64} onClick={() => setDraft({ ...draft, steps: [...draft.steps, { preset_token: "", dwell_seconds: 10, speed: 0.5 }] })}>Agregar paso</Button>
            <Button size="sm" variant="primary" type="submit" disabled={!valid || save.isPending}>{save.isPending ? "Guardando…" : "Guardar tour"}</Button>
            <Button size="sm" onClick={() => setDraft(null)}>Cancelar</Button>
          </div>
          <ErrorNote error={save.error} />
        </form>
      )}
    </section>
  );
}
