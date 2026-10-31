import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  CircleSlash,
  Clock,
  Cpu,
  FlaskConical,
  Lightbulb,
  Loader2,
  Radio,
  XCircle,
} from "lucide-react";
import { requestJson, type Schemas } from "@/api/client";
import { Icon } from "@/components/Icon";
import { Button, ErrorNote } from "@/components/ui";
import { cn } from "@/lib/cn";
import { fmtDateTime } from "@/lib/format";

type LprTest = Schemas["CameraLprTest"];
type CheckStatus = LprTest["checks"][number]["status"];

/** How long a Provision test waits for a fresh keepalive: the cameras send one every 20 s. */
const WAIT_SECONDS = 20;

const statusStyle: Record<
  CheckStatus,
  {
    icon: typeof CheckCircle2;
    iconClass: string;
    badgeClass: string;
    borderClass: string;
    label: string;
    badgeText: string;
  }
> = {
  pass: {
    icon: CheckCircle2,
    iconClass: "text-emerald-500",
    badgeClass: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20",
    borderClass: "border-emerald-500/30 bg-emerald-500/[0.03]",
    label: "Correcto",
    badgeText: "OK",
  },
  fail: {
    icon: XCircle,
    iconClass: "text-rose-500",
    badgeClass: "bg-rose-500/10 text-rose-600 dark:text-rose-400 border-rose-500/20",
    borderClass: "border-rose-500/30 bg-rose-500/[0.03]",
    label: "Falló",
    badgeText: "FALLÓ",
  },
  warn: {
    icon: AlertTriangle,
    iconClass: "text-amber-500",
    badgeClass: "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20",
    borderClass: "border-amber-500/30 bg-amber-500/[0.03]",
    label: "Advertencia",
    badgeText: "AVISO",
  },
  skip: {
    icon: CircleSlash,
    iconClass: "text-muted",
    badgeClass: "bg-surface-3 text-muted border-outline-variant/40",
    borderClass: "border-outline-variant/30 bg-surface-2/40",
    label: "Omitido",
    badgeText: "OMITIDO",
  },
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
      requestJson<LprTest>("POST", `/api/v1/cameras/${encodeURIComponent(cameraId)}/lpr/test`, {
        wait_seconds: waitSeconds,
      }),
  });
  const waiting = test.isPending && canWait && wait;
  const result = test.data;

  return (
    <div className="flex flex-col gap-4 rounded-m3-2xl border border-outline-variant/50 bg-surface-1 p-5 shadow-sm">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-outline-variant/30 pb-3">
        <div className="flex items-center gap-2.5">
          <div className="flex size-9 items-center justify-center rounded-xl bg-primary/10 text-primary">
            <Icon icon={FlaskConical} size="sm" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h3 className="text-sm font-bold text-on-surface">Probar conexión de lectura de patentes</h3>
              {engine && (
                <span className="rounded-full bg-surface-2 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-muted">
                  {engine === "provision" ? "Provision-ISR" : engine === "frigate" ? "Frigate NVR" : "General"}
                </span>
              )}
            </div>
            <p className="text-xs text-muted">Diagnóstico punto a punto de la recepción de patentes y latidos</p>
          </div>
        </div>

        <Button
          size="sm"
          variant="filled"
          onClick={() => test.mutate(canWait && wait ? WAIT_SECONDS : 0)}
          disabled={test.isPending}
        >
          {test.isPending ? <Icon icon={Loader2} size="xs" className="animate-spin" /> : <Icon icon={FlaskConical} size="xs" />}
          {waiting ? "Esperando keepalive…" : test.isPending ? "Probando…" : "Probar conexión"}
        </Button>
      </div>

      {canWait && (
        <label className="flex items-center gap-2.5 rounded-xl border border-outline-variant/40 bg-surface-2/60 px-3 py-2 text-xs text-on-surface cursor-pointer hover:bg-surface-2 transition-colors">
          <input
            type="checkbox"
            className="size-4 rounded accent-primary cursor-pointer"
            checked={wait}
            disabled={test.isPending}
            onChange={(e) => setWait(e.target.checked)}
          />
          <span>Esperar hasta {WAIT_SECONDS} s a que llegue un keepalive de la cámara</span>
        </label>
      )}

      {!result && !test.isPending && !test.error && (
        <div className="rounded-xl border border-dashed border-outline-variant/60 p-4 text-center">
          <p className="text-xs text-muted">
            Comprueba cada tramo del camino de las lecturas, sin modificar nada. Es de solo lectura.
          </p>
        </div>
      )}

      {test.error && <ErrorNote error={test.error instanceof Error ? test.error : new Error(String(test.error))} />}

      {result && (
        <div className="flex flex-col gap-4">
          <div
            role="status"
            className={cn(
              "flex items-center gap-2.5 rounded-xl border p-3.5 text-xs font-bold",
              result.engine === "none"
                ? "border-outline-variant/50 bg-surface-2 text-muted"
                : result.passed
                ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
                : "border-rose-500/30 bg-rose-500/10 text-rose-600 dark:text-rose-400"
            )}
          >
            <Icon
              icon={
                result.engine === "none"
                  ? CircleSlash
                  : result.passed
                  ? CheckCircle2
                  : XCircle
              }
              size="sm"
            />
            <span>
              {result.engine === "none"
                ? "Esta cámara no lee patentes."
                : result.passed
                ? "La conexión funciona."
                : "Hay pasos que fallan."}
            </span>
          </div>

          <div className="flex flex-col gap-2">
            <span className="text-[11px] font-bold uppercase tracking-wider text-muted">Chequeos Realizados</span>
            <ul className="flex flex-col gap-2">
              {result.checks.map((c) => {
                const s = statusStyle[c.status];
                return (
                  <li
                    key={c.id}
                    data-status={c.status}
                    className={cn(
                      "flex items-start justify-between gap-3 rounded-xl border p-3 text-xs transition-colors",
                      s.borderClass
                    )}
                  >
                    <div className="flex items-start gap-2.5">
                      <Icon icon={s.icon} size="xs" className={cn("mt-0.5 shrink-0", s.iconClass)} />
                      <div className="flex flex-col gap-0.5">
                        <span className="sr-only">{s.label}: </span>
                        <span className="font-semibold text-on-surface">{c.message}</span>
                        {c.detail && <span className="font-mono text-[11px] text-muted">{c.detail}</span>}
                      </div>
                    </div>
                    <span
                      className={cn(
                        "shrink-0 rounded-full border px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider",
                        s.badgeClass
                      )}
                    >
                      {s.badgeText}
                    </span>
                  </li>
                );
              })}
            </ul>
          </div>

          {result.hints.length > 0 && (
            <div className="flex flex-col gap-2 rounded-xl border border-primary/30 bg-primary/5 p-3.5 text-xs">
              <div className="flex items-center gap-1.5 font-bold uppercase tracking-wider text-primary">
                <Icon icon={Lightbulb} size="xs" />
                <span>Qué hacer</span>
              </div>
              <ul className="list-disc pl-5 text-on-surface space-y-1">
                {result.hints.map((h) => (
                  <li key={h}>{h}</li>
                ))}
              </ul>
            </div>
          )}

          <div className="flex flex-col gap-2 pt-1 border-t border-outline-variant/30">
            <span className="text-[11px] font-bold uppercase tracking-wider text-muted">Métricas y Contadores Observados</span>
            <ObservedCounters observed={result.observed} />
          </div>
        </div>
      )}
    </div>
  );
}

function ObservedCounters({ observed }: { observed: LprTest["observed"] }) {
  const rows: [string, string, typeof Radio][] = [];
  if (observed.last_seen) rows.push(["Último keepalive", fmtDateTime(observed.last_seen), Clock]);
  if (observed.keepalive_count !== undefined) rows.push(["Keepalives", String(observed.keepalive_count), Radio]);
  if (observed.event_count !== undefined) rows.push(["Lecturas en el agente", String(observed.event_count), Cpu]);
  if (observed.last_plate) rows.push(["Última patente", observed.last_plate, Activity]);
  if (observed.last_event_time) rows.push(["Hora de la última lectura", fmtDateTime(observed.last_event_time), Clock]);
  if (observed.reads_24h !== undefined) rows.push(["Lecturas en 24 h", String(observed.reads_24h), Activity]);
  if (observed.last_read_at) rows.push(["Última lectura guardada", fmtDateTime(observed.last_read_at), Clock]);
  if (rows.length === 0) return null;

  return (
    <dl className="grid grid-cols-2 gap-2.5 sm:grid-cols-3">
      {rows.map(([k, v, RowIcon]) => (
        <div key={k} className="flex flex-col rounded-xl border border-outline-variant/40 bg-surface-2 p-2.5">
          <div className="flex items-center gap-1.5 text-muted">
            <Icon icon={RowIcon} size="xs" className="opacity-70" />
            <dt className="text-[11px] truncate">{k}</dt>
          </div>
          <dd className="font-mono text-sm font-bold text-on-surface mt-1 truncate">{v}</dd>
        </div>
      ))}
    </dl>
  );
}
