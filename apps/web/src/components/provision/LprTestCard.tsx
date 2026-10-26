import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2, CircleSlash, FlaskConical, Loader2, XCircle } from "lucide-react";
import { requestJson, type Schemas } from "@/api/client";
import { Icon } from "@/components/Icon";
import { Button, ErrorNote } from "@/components/ui";
import { cn } from "@/lib/cn";
import { fmtDateTime } from "@/lib/format";

type LprTest = Schemas["CameraLprTest"];
type CheckStatus = LprTest["checks"][number]["status"];

/** How long a Provision test waits for a fresh keepalive: the cameras send one every 20 s. */
const WAIT_SECONDS = 20;

const statusStyle: Record<CheckStatus, { icon: typeof CheckCircle2; className: string; label: string }> = {
  pass: { icon: CheckCircle2, className: "text-ok", label: "Correcto" },
  fail: { icon: XCircle, className: "text-error", label: "Falló" },
  warn: { icon: AlertTriangle, className: "text-warn", label: "Advertencia" },
  skip: { icon: CircleSlash, className: "text-muted", label: "Omitido" },
};

interface LprTestCardProps {
  cameraId: string;
  /** The engine known beforehand; "provision" offers to wait for a keepalive. The answer carries the real one. */
  engine?: "provision" | "frigate" | "none";
}

/**
 * LprTestCard runs the read-only check of a camera's plate chain ("Probar conexión") and lists every
 * step with its outcome, what to do about it, and the counters observed. It never changes anything:
 * the check only observes what the camera, the agent and Frigate already report.
 */
export function LprTestCard({ cameraId, engine }: LprTestCardProps) {
  const canWait = engine === "provision";
  const [wait, setWait] = useState(canWait);
  const test = useMutation({
    mutationFn: (waitSeconds: number) =>
      requestJson<LprTest>("POST", `/api/v1/cameras/${encodeURIComponent(cameraId)}/lpr/test`, { wait_seconds: waitSeconds }),
  });
  const waiting = test.isPending && canWait && wait;
  const result = test.data;

  return (
    <div className="flex flex-col gap-3 rounded-m3-2xl border border-outline-variant/50 bg-surface-1 p-5 shadow-sm">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Icon icon={FlaskConical} size="sm" className="text-primary" />
          <h3 className="text-sm font-bold text-on-surface">Probar conexión de lectura de patentes</h3>
        </div>
        <Button size="sm" onClick={() => test.mutate(canWait && wait ? WAIT_SECONDS : 0)} disabled={test.isPending}>
          {test.isPending ? <Icon icon={Loader2} size="xs" className="animate-spin" /> : <Icon icon={FlaskConical} size="xs" />}
          {waiting ? "Esperando keepalive…" : test.isPending ? "Probando…" : "Probar conexión"}
        </Button>
      </div>

      {canWait && (
        <label className="flex items-center gap-2 text-xs text-muted">
          <input type="checkbox" className="size-4 accent-primary" checked={wait} disabled={test.isPending} onChange={(e) => setWait(e.target.checked)} />
          Esperar hasta {WAIT_SECONDS} s a que llegue un keepalive de la cámara
        </label>
      )}

      {!result && !test.isPending && !test.error && (
        <p className="text-xs text-muted">
          Comprueba cada tramo del camino de las lecturas, sin modificar nada. Es de solo lectura.
        </p>
      )}

      {test.error && <ErrorNote error={test.error instanceof Error ? test.error : new Error(String(test.error))} />}

      {result && (
        <>
          <p role="status" className={cn("text-xs font-bold", result.passed ? "text-ok" : "text-error")}>
            {result.engine === "none" ? "Esta cámara no lee patentes." : result.passed ? "La conexión funciona." : "Hay pasos que fallan."}
          </p>
          <ul className="flex flex-col gap-1.5">
            {result.checks.map((c) => {
              const s = statusStyle[c.status];
              return (
                <li key={c.id} data-status={c.status} className="flex items-start gap-2 rounded-m3-lg bg-surface-2 p-2.5 text-xs">
                  <Icon icon={s.icon} size="xs" className={cn("mt-0.5 shrink-0", s.className)} />
                  <div className="flex flex-col gap-0.5">
                    <span className="sr-only">{s.label}: </span>
                    <span className="font-semibold text-on-surface">{c.message}</span>
                    {c.detail && <span className="text-muted">{c.detail}</span>}
                  </div>
                </li>
              );
            })}
          </ul>

          {result.hints.length > 0 && (
            <div className="flex flex-col gap-1 rounded-m3-lg border border-outline-variant/40 p-2.5 text-xs">
              <span className="font-bold uppercase tracking-wider text-muted">Qué hacer</span>
              <ul className="list-disc pl-4 text-on-surface">
                {result.hints.map((h) => (
                  <li key={h}>{h}</li>
                ))}
              </ul>
            </div>
          )}

          <ObservedCounters observed={result.observed} />
        </>
      )}
    </div>
  );
}

function ObservedCounters({ observed }: { observed: LprTest["observed"] }) {
  const rows: [string, string][] = [];
  if (observed.last_seen) rows.push(["Último keepalive", fmtDateTime(observed.last_seen)]);
  if (observed.keepalive_count !== undefined) rows.push(["Keepalives", String(observed.keepalive_count)]);
  if (observed.event_count !== undefined) rows.push(["Lecturas en el agente", String(observed.event_count)]);
  if (observed.last_plate) rows.push(["Última patente", observed.last_plate]);
  if (observed.last_event_time) rows.push(["Hora de la última lectura", fmtDateTime(observed.last_event_time)]);
  if (observed.reads_24h !== undefined) rows.push(["Lecturas en 24 h", String(observed.reads_24h)]);
  if (observed.last_read_at) rows.push(["Última lectura guardada", fmtDateTime(observed.last_read_at)]);
  if (rows.length === 0) return null;
  return (
    <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs sm:grid-cols-3">
      {rows.map(([k, v]) => (
        <div key={k} className="flex flex-col">
          <dt className="text-muted">{k}</dt>
          <dd className="font-mono font-semibold text-on-surface">{v}</dd>
        </div>
      ))}
    </dl>
  );
}
