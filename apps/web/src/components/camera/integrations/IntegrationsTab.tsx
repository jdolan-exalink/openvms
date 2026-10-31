import { useState } from "react";
import {
  Activity,
  AlertTriangle,
  Camera,
  CheckCircle2,
  Cpu,
  FlaskConical,
  Loader2,
  Radio,
  Server,
  X,
  XCircle,
} from "lucide-react";
import { requestJson } from "@/api/client";
import { Button } from "@/components/ui";
import { cn } from "@/lib/cn";

export type IntegrationStatus = "connected" | "not_configured" | "error" | "unknown";

/** One backend integration of the normalized configuration; entries without an id are plain feature flags. */
export type IntegrationEntry = { id?: string; label?: string; status?: IntegrationStatus; detail?: string };

const STATUS_TEXT: Record<IntegrationStatus, string> = {
  connected: "Conectado",
  not_configured: "No configurado",
  error: "Error",
  unknown: "Sin verificar",
};

const STATUS_DOT: Record<IntegrationStatus, string> = {
  connected: "bg-emerald-500",
  not_configured: "bg-muted",
  error: "bg-rose-500",
  unknown: "bg-amber-500",
};

const STATUS_BADGE: Record<IntegrationStatus, string> = {
  connected: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20",
  not_configured: "bg-surface-3 text-muted border-outline-variant/40",
  error: "bg-rose-500/10 text-rose-600 dark:text-rose-400 border-rose-500/20",
  unknown: "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20",
};

export type Backend = { id: string; label: string; status: IntegrationStatus; detail?: string };

/** The backend integrations in display order; other entries are not shown. */
export function backendIntegrations(list: readonly IntegrationEntry[] | undefined): Backend[] {
  const out: Backend[] = [];
  for (const i of list ?? []) if (i.id && i.label && i.status) out.push({ id: i.id, label: i.label, status: i.status, detail: i.detail });
  return out;
}

const TESTABLE_TARGETS = new Set(["lpr_provision", "analytics_provision", "analytics_frigate", "lpr_frigate"]);

type AnalyticsCheck = {
  id: string;
  status: "pass" | "fail" | "warn" | "skip";
  message: string;
  detail?: string;
};

type AnalyticsTestResult = {
  target: string;
  engine: string;
  passed: boolean;
  checks: AnalyticsCheck[];
  hints?: string[];
  observed?: Record<string, unknown>;
};

function serviceIcon(id: string) {
  switch (id) {
    case "lpr_provision":
    case "lpr_frigate":
      return <Camera className="size-4 text-primary shrink-0" aria-hidden="true" />;
    case "analytics_provision":
    case "analytics_frigate":
      return <Activity className="size-4 text-primary shrink-0" aria-hidden="true" />;
    case "onvif":
      return <Radio className="size-4 text-purple-400 shrink-0" aria-hidden="true" />;
    case "frigate":
      return <Server className="size-4 text-sky-400 shrink-0" aria-hidden="true" />;
    case "provision_isr":
      return <Cpu className="size-4 text-amber-400 shrink-0" aria-hidden="true" />;
    default:
      return <Activity className="size-4 text-muted shrink-0" aria-hidden="true" />;
  }
}

function serviceOriginBadge(id: string) {
  if (id.includes("provision")) {
    return <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[10px] font-semibold text-amber-300">Provision</span>;
  }
  if (id.includes("frigate")) {
    return <span className="rounded-full border border-sky-500/30 bg-sky-500/10 px-2 py-0.5 text-[10px] font-semibold text-sky-400">Frigate</span>;
  }
  if (id === "onvif") {
    return <span className="rounded-full border border-purple-500/30 bg-purple-500/10 px-2 py-0.5 text-[10px] font-semibold text-purple-300">ONVIF</span>;
  }
  return null;
}

type Props = {
  integrations: readonly IntegrationEntry[] | undefined;
  cameraId?: string;
  readOnly?: boolean;
};

/**
 * Status and diagnostic controls of the integrations and analytics services wired to the camera.
 * Supports interactive testing ("Probar") for LPR and smart analytics.
 */
export function IntegrationsTab({ integrations, cameraId, readOnly }: Props) {
  const [testingTarget, setTestingTarget] = useState<string | null>(null);
  const [testResults, setTestResults] = useState<Record<string, AnalyticsTestResult>>({});
  const [errorNotes, setErrorNotes] = useState<Record<string, string>>({});

  const handleTest = async (target: string) => {
    if (!cameraId) return;
    setTestingTarget(target);
    try {
      const data = await requestJson<AnalyticsTestResult>("POST", `/api/v1/cameras/${cameraId}/analytics/test`, {
        target,
      });
      setTestResults((prev) => ({ ...prev, [target]: data }));
      setErrorNotes((prev) => {
        const next = { ...prev };
        delete next[target];
        return next;
      });
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : "Error al realizar la prueba";
      setErrorNotes((prev) => ({ ...prev, [target]: message }));
    } finally {
      setTestingTarget(null);
    }
  };

  const dismissResult = (target: string) => {
    setTestResults((prev) => {
      const next = { ...prev };
      delete next[target];
      return next;
    });
    setErrorNotes((prev) => {
      const next = { ...prev };
      delete next[target];
      return next;
    });
  };

  if (!integrations) return <p className="text-sm text-muted">Cargando integraciones…</p>;
  const rows = backendIntegrations(integrations);
  if (rows.length === 0) return <p className="text-sm text-muted">Sin información de integraciones.</p>;

  return (
    <ul className="flex flex-col gap-2.5" aria-label="Integraciones">
      {rows.map((i) => {
        const isTestable = TESTABLE_TARGETS.has(i.id) && !!cameraId && !readOnly;
        const isTesting = testingTarget === i.id;
        const result = testResults[i.id];
        const error = errorNotes[i.id];

        return (
          <li
            key={i.id}
            data-testid={`integration-${i.id}`}
            className="flex flex-col gap-3 rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-3.5 transition-colors"
          >
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex items-center gap-2.5 min-w-0">
                {serviceIcon(i.id)}
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-bold text-on-surface">{i.label}</span>
                  {serviceOriginBadge(i.id)}
                </div>
              </div>

              <div className="flex items-center gap-2">
                <span
                  className={cn(
                    "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-semibold",
                    STATUS_BADGE[i.status],
                  )}
                >
                  <span aria-hidden className={cn("size-2 rounded-full", STATUS_DOT[i.status])} />
                  {STATUS_TEXT[i.status]}
                </span>

                {isTestable && (
                  <Button
                    type="button"
                    variant="tonal"
                    size="sm"
                    disabled={isTesting}
                    onClick={() => handleTest(i.id)}
                    className="inline-flex items-center gap-1.5 text-xs font-semibold"
                  >
                    {isTesting ? (
                      <>
                        <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
                        <span>Probando…</span>
                      </>
                    ) : (
                      <>
                        <FlaskConical className="size-3.5" aria-hidden="true" />
                        <span>Probar</span>
                      </>
                    )}
                  </Button>
                )}
              </div>
            </div>

            {i.detail && <p className="text-xs text-muted pl-6">{i.detail}</p>}

            {/* Test result diagnostics panel */}
            {result && (
              <div
                className={cn(
                  "relative mt-1 flex flex-col gap-2 rounded-m3-lg border p-3 text-xs",
                  result.passed
                    ? "border-emerald-500/30 bg-emerald-500/[0.04]"
                    : "border-rose-500/30 bg-rose-500/[0.04]",
                )}
              >
                <button
                  type="button"
                  aria-label="Cerrar resultado"
                  onClick={() => dismissResult(i.id)}
                  className="absolute right-2 top-2 p-1 text-muted hover:text-on-surface"
                >
                  <X className="size-3.5" />
                </button>

                <div className="flex items-center gap-2 font-bold">
                  {result.passed ? (
                    <>
                      <CheckCircle2 className="size-4 text-emerald-500" />
                      <span className="text-emerald-400">Prueba exitosa: servicio operativo</span>
                    </>
                  ) : (
                    <>
                      <XCircle className="size-4 text-rose-500" />
                      <span className="text-rose-400">Atención: se encontraron problemas</span>
                    </>
                  )}
                </div>

                {result.checks && result.checks.length > 0 && (
                  <ul className="flex flex-col gap-1.5 pt-1">
                    {result.checks.map((chk) => (
                      <li key={chk.id} className="flex items-start gap-2 text-xs">
                        {chk.status === "pass" ? (
                          <CheckCircle2 className="size-3.5 text-emerald-500 shrink-0 mt-0.5" />
                        ) : chk.status === "fail" ? (
                          <XCircle className="size-3.5 text-rose-500 shrink-0 mt-0.5" />
                        ) : chk.status === "warn" ? (
                          <AlertTriangle className="size-3.5 text-amber-500 shrink-0 mt-0.5" />
                        ) : (
                          <span className="size-3.5 rounded-full bg-muted/30 shrink-0 mt-0.5" />
                        )}
                        <div className="flex flex-wrap items-baseline gap-1.5">
                          <span className="font-semibold text-on-surface">{chk.message}</span>
                          {chk.detail && <span className="text-muted">({chk.detail})</span>}
                        </div>
                      </li>
                    ))}
                  </ul>
                )}

                {result.hints && result.hints.length > 0 && (
                  <div className="mt-1 rounded border border-amber-500/20 bg-amber-500/5 p-2 text-[11px] text-amber-300">
                    <ul className="list-disc pl-4 space-y-0.5">
                      {result.hints.map((h, idx) => (
                        <li key={idx}>{h}</li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            )}

            {error && (
              <div className="relative mt-1 flex items-center justify-between rounded-m3-lg border border-rose-500/30 bg-rose-500/10 p-2.5 text-xs text-rose-400">
                <span>{error}</span>
                <button
                  type="button"
                  aria-label="Cerrar error"
                  onClick={() => dismissResult(i.id)}
                  className="p-1 text-muted hover:text-on-surface"
                >
                  <X className="size-3.5" />
                </button>
              </div>
            )}
          </li>
        );
      })}
    </ul>
  );
}
