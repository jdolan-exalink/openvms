import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { Modal } from "@/components/Modal";
import { PatchFields } from "@/components/camera/profile/PatchFields";
import { draftErrors, draftToPatch, emptyDraft, leafCount, type PatchDraft, type Section, sectionLabel, sections } from "@/components/camera/profile/patchForm";
import { Button, ErrorNote, Pill } from "@/components/ui";

type Report = Schemas["CameraConfigBulkReport"];
type Row = Report["rows"][number];

const statusText: Record<Row["status"], string> = {
  will_apply: "Se aplicará",
  applied: "Aplicada",
  failed: "Falló",
  incompatible: "Incompatible",
  skipped: "Omitida",
  unreachable: "Sin conexión",
};
const statusTone = { will_apply: "info", applied: "ok", failed: "bad", incompatible: "warn", skipped: "neutral", unreachable: "bad" } as const;

const fmt = (v: unknown): string => (v === undefined || v === null ? "—" : typeof v === "boolean" ? (v ? "ON" : "OFF") : Array.isArray(v) ? v.join(", ") : typeof v === "object" ? JSON.stringify(v) : String(v));
const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/** The cameras a bulk change targets: a group (resolved by the server) or an explicit list. */
export type BulkTarget = { groupId: string; cameraIds?: never } | { cameraIds: string[]; groupId?: never };

function ReportTable({ rows }: { rows: Row[] }) {
  return (
    <div className="max-h-72 overflow-auto rounded-m3-lg border border-outline-variant">
      <table className="w-full text-left text-sm" aria-label="Cámaras">
        <thead className="sticky top-0 bg-surface-2 text-xs"><tr><th className="p-2">Cámara</th><th className="p-2">Estado</th><th className="p-2">Detalle</th></tr></thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.camera_id} className="border-t border-outline-variant align-top">
              <td className="p-2">{r.name || "Cámara sin acceso"}</td>
              <td className="p-2"><Pill size="sm" tone={statusTone[r.status]}>{statusText[r.status]}</Pill></td>
              <td className="p-2 text-xs text-muted">
                {r.reason && <div>{r.reason}</div>}
                {r.changes.map((c) => <div key={c.path}>{c.label}: {fmt(c.from)} → {fmt(c.to)}</div>)}
                {r.impact.map((i) => <div key={i.code}>{i.message}</div>)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * "Configurar cámaras": apply ONE section of the configuration to many cameras. Step 1 picks the
 * section and its values, step 2 reviews who is affected, step 3 shows the per-camera outcome.
 * Parameters outside the chosen section are never sent, so they are never overwritten.
 */
export function BulkConfigureDialog({ target, targetLabel, onClose, onDone }: { target: BulkTarget; targetLabel?: string; onClose: () => void; onDone?: () => void }) {
  const [step, setStep] = useState<"choose" | "review" | "done">("choose");
  const [section, setSection] = useState<Section>("detection");
  const [draft, setDraft] = useState<PatchDraft>(emptyDraft());
  const patch = draftToPatch(draft, section);
  const valid = leafCount(patch) > 0 && Object.keys(draftErrors(draft)).length === 0;
  const body = { ...(target.groupId ? { group_id: target.groupId } : { camera_ids: target.cameraIds }), patch, origin: (target.groupId ? "group" : "api") as "group" | "api" };

  const review = useMutation({ mutationFn: async () => unwrap(await api.POST("/api/v1/camera-config/bulk/review", { body })) });
  const apply = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/camera-config/bulk/apply", { body })),
    onSuccess: () => { setStep("done"); onDone?.(); },
  });

  const rep: Report | undefined = step === "done" ? apply.data : review.data;
  const willApply = review.data?.totals.will_apply ?? 0;
  const label = sectionLabel[section].toLowerCase();

  return (
    <Modal title={targetLabel ? `Configurar cámaras: ${targetLabel}` : "Configurar cámaras"} onClose={onClose}>
      {step === "choose" && (
        <>
          <fieldset className="flex flex-wrap gap-x-6" aria-label="Sección a configurar">
            <legend className="mb-1 text-sm font-semibold">Sección (una por vez)</legend>
            {sections.map((s) => (
              <label key={s} className="flex min-h-11 items-center gap-2 text-sm">
                <input type="radio" name="bulk-section" className="size-5 accent-primary" checked={section === s} onChange={() => setSection(s)} />
                {sectionLabel[s]}
              </label>
            ))}
          </fieldset>
          <p className="text-xs text-muted">Solo se cambia lo que completes en esta sección; el resto de los parámetros de cada cámara no se toca.</p>
          <PatchFields draft={draft} onChange={setDraft} only={section} />
          <div className="flex justify-end gap-2">
            <Button variant="text" onClick={onClose}>Cancelar</Button>
            <Button variant="primary" disabled={!valid} onClick={() => { setStep("review"); review.mutate(); }}>Revisar</Button>
          </div>
        </>
      )}

      {step !== "choose" && (
        <>
          {step === "review" && review.isPending && <p role="status" className="text-sm text-muted">Calculando cámaras afectadas…</p>}
          {rep && (
            <>
              {step === "review" ? (
                <p className="text-sm font-semibold" data-testid="bulk-total">Aplicar {label} a {plural(willApply, "cámara", "cámaras")}</p>
              ) : (
                <p className="text-sm font-semibold" role="status" data-testid="bulk-total">
                  {plural(rep.totals.applied, "cámara actualizada", "cámaras actualizadas")}{rep.totals.failed > 0 ? `, ${plural(rep.totals.failed, "falló", "fallaron")}` : ""}.
                </p>
              )}
              <ul className="flex flex-wrap gap-2 text-xs" aria-label="Resumen">
                <li><Pill tone="info" size="sm">Cámaras afectadas: {step === "done" ? rep.totals.applied + rep.totals.failed : rep.totals.will_apply}</Pill></li>
                <li><Pill tone="warn" size="sm">Incompatibles: {rep.totals.incompatible}</Pill></li>
                <li><Pill size="sm">Omitidas: {rep.totals.skipped}</Pill></li>
                {rep.totals.unreachable > 0 && <li><Pill tone="bad" size="sm">Sin conexión: {rep.totals.unreachable}</Pill></li>}
                <li><Pill tone={rep.restarts > 0 ? "warn" : "neutral"} size="sm">Reinicios necesarios: {rep.restarts}</Pill></li>
              </ul>
              <ReportTable rows={rep.rows} />
            </>
          )}
          <ErrorNote error={review.error ?? apply.error} />
          <div className="flex justify-end gap-2">
            {step === "review" && <Button variant="text" onClick={() => { review.reset(); apply.reset(); setStep("choose"); }}>Atrás</Button>}
            <Button variant="text" onClick={onClose}>{step === "done" ? "Cerrar" : "Cancelar"}</Button>
            {step === "review" && (
              <Button variant="primary" disabled={!review.data || willApply === 0 || apply.isPending} onClick={() => apply.mutate()}>
                {apply.isPending ? "Aplicando…" : "Aplicar"}
              </Button>
            )}
          </div>
        </>
      )}
    </Modal>
  );
}
