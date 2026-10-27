import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { Button, Pill } from "@/components/ui";

type Review = Schemas["CameraConfigProfileReview"];
type Entry = { id: string; name: string; review?: Review; error?: string; outcome?: "applied" | "failed"; outcomeError?: string };

/** Most outdated cameras one re-apply round reviews, so a huge profile never fires hundreds of calls. */
export const REAPPLY_CAP = 50;

const message = (e: unknown) => (e instanceof Error ? e.message : "Error desconocido");

/**
 * One-click re-apply of a profile to its outdated followers. Reviews each camera sequentially
 * (no side effects), shows a single summary table and applies only after one explicit confirmation,
 * continuing past a camera that fails.
 */
export function ReapplyOutdated({ profileId, cameras }: { profileId: string; cameras: { id: string; name: string }[] }) {
  const qc = useQueryClient();
  const [entries, setEntries] = useState<Entry[] | null>(null);
  const [busy, setBusy] = useState<"review" | "apply" | null>(null);
  const targets = cameras.slice(0, REAPPLY_CAP);
  const patch = (id: string, change: Partial<Entry>) => setEntries((cur) => cur?.map((e) => (e.id === id ? { ...e, ...change } : e)) ?? cur);

  async function review() {
    setBusy("review");
    setEntries(targets.map((c) => ({ id: c.id, name: c.name })));
    for (const c of targets) {
      try {
        const r = unwrap(await api.POST("/api/v1/cameras/{cameraId}/profile/review", { params: { path: { cameraId: c.id } }, body: { profile_id: profileId, keep_values: true } }));
        patch(c.id, { review: r });
      } catch (e) {
        patch(c.id, { error: message(e) });
      }
    }
    setBusy(null);
  }

  const ready = (entries ?? []).filter((e) => e.review && !e.outcome);
  async function apply() {
    setBusy("apply");
    for (const e of ready) {
      try {
        unwrap(await api.POST("/api/v1/cameras/{cameraId}/profile/apply", { params: { path: { cameraId: e.id } }, body: { profile_id: profileId, keep_values: true } }));
        patch(e.id, { outcome: "applied" });
      } catch (err) {
        patch(e.id, { outcome: "failed", outcomeError: message(err) });
      }
    }
    setBusy(null);
    await qc.invalidateQueries({ queryKey: ["camera-config", "profiles"] });
  }

  if (!entries) {
    return <Button size="sm" className="self-start" onClick={review}>Reaplicar a las desactualizadas</Button>;
  }
  const done = entries.some((e) => e.outcome);
  return (
    <div className="flex flex-col gap-2">
      <table aria-label="Resumen de reaplicación" className="w-full text-left text-xs">
        <thead><tr><th className="p-1">Cámara</th><th className="p-1">Parámetros</th><th className="p-1">Impacto</th><th className="p-1">Resultado</th></tr></thead>
        <tbody>
          {entries.map((e) => (
            <tr key={e.id} className="border-t border-outline-variant align-top">
              <td className="p-1">{e.name}</td>
              <td className="p-1">{e.review ? e.review.parameters : "—"}</td>
              <td className="p-1 text-muted">{e.review?.review.impact.map((i) => <div key={i.code}>{i.message}</div>)}</td>
              <td className="p-1">
                {e.error && <span role="alert" className="text-bad">{e.error}</span>}
                {e.outcome === "applied" && <Pill size="sm" tone="ok">Aplicada</Pill>}
                {e.outcome === "failed" && <span role="alert" className="text-bad">Falló: {e.outcomeError}</span>}
                {!e.review && !e.error && <span className="text-muted">Revisando…</span>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {cameras.length > REAPPLY_CAP && <p className="text-xs text-warn">Se revisan las primeras {REAPPLY_CAP} de {cameras.length} cámaras; repetí para el resto.</p>}
      {!done && <Button size="sm" variant="primary" className="self-start" disabled={busy !== null || ready.length === 0} onClick={apply}>{busy === "apply" ? "Aplicando…" : `Aplicar a ${ready.length} cámaras`}</Button>}
    </div>
  );
}
