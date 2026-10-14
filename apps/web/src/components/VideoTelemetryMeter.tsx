import { ChevronDown, ChevronUp } from "lucide-react";
import { useMemo, useState } from "react";
import { useVideoTelemetry } from "@/components/clientStation/hooks";
import { HEALTH_KEYS, HEALTH_TONES } from "@/components/clientStation/keys";
import { Icon } from "@/components/Icon";
import { Pill } from "@/components/ui";
import { useT } from "@/i18n";
import { startupProfile, type HealthState, type TelemetryRuntime, type VideoPerformanceMetrics } from "@/lib/telemetry";

export const TELEMETRY_COLLAPSED_KEY = "openvms.live.telemetryMeter.collapsed";

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(TELEMETRY_COLLAPSED_KEY) === "1";
  } catch {
    return false;
  }
}

function writeCollapsed(collapsed: boolean): void {
  try {
    localStorage.setItem(TELEMETRY_COLLAPSED_KEY, collapsed ? "1" : "0");
  } catch {
    // storage unavailable
  }
}

const CHART_W = 120;
const CHART_H = 28;
export const HISTORY_SIZE = 60;

/** Plain-SVG bar histogram of the last minute of Mbps (at most HISTORY_SIZE bars). */
function Histogram({ history, peak }: { history: readonly VideoPerformanceMetrics[]; peak: number }) {
  const points = history.slice(-HISTORY_SIZE);
  const scale = Math.max(peak, 1);
  const slot = CHART_W / HISTORY_SIZE;
  return (
    <svg
      role="img"
      aria-label={`Histograma de ancho de banda de los últimos 60 s, máximo ${peak.toFixed(1)} Mbps`}
      viewBox={`0 0 ${CHART_W} ${CHART_H}`}
      preserveAspectRatio="none"
      className="h-7 w-full text-primary"
    >
      {points.map((p, i) => {
        const mbps = p.receivedMbps;
        const h = Math.max((mbps / scale) * CHART_H, mbps > 0 ? 1 : 0);
        return (
          <rect
            key={p.timestamp}
            data-testid="telemetry-bar"
            x={(HISTORY_SIZE - points.length + i) * slot}
            y={CHART_H - h}
            width={Math.max(slot - 0.4, 0.5)}
            height={h}
            rx={0.4}
            fill="currentColor"
            opacity={i === points.length - 1 ? 1 : 0.55}
          />
        );
      })}
    </svg>
  );
}

function Stat({ label, value, title }: { label: string; value: string; title?: string }) {
  return (
    <div className="flex min-w-0 flex-col" title={title}>
      <dt className="text-[10px] uppercase tracking-wide text-on-surface-variant">{label}</dt>
      <dd className="truncate font-medium tabular-nums text-on-surface">{value}</dd>
    </div>
  );
}

export function gpuShortName(renderer?: string): string | null {
  if (!renderer) return null;
  const match = renderer.match(/(?:ANGLE\s*\(.*?,\s*|Direct3D11\s+vs_\d+_\d+\s+ps_\d+_\d+\s+)?([^,()]+)(?:\s+Direct3D|\s+vs_|\s+OpenGL|\s*\()/i);
  return (match?.[1] ?? renderer).trim().slice(0, 32);
}

function detectQuickGpu(): string | undefined {
  try {
    const canvas = document.createElement("canvas");
    const gl = canvas.getContext("webgl2") || canvas.getContext("webgl");
    if (!gl) return undefined;
    const dbg = (gl as WebGLRenderingContext).getExtension("WEBGL_debug_renderer_info");
    const renderer = dbg
      ? (gl as WebGLRenderingContext).getParameter(dbg.UNMASKED_RENDERER_WEBGL)
      : (gl as WebGLRenderingContext).getParameter((gl as WebGLRenderingContext).RENDERER);
    return typeof renderer === "string" ? renderer : undefined;
  } catch {
    return undefined;
  }
}

/** Compact live-video meter pinned to the bottom of the camera sidebar. Follows 1 Hz telemetry ticks. */
export function VideoTelemetryMeter({ runtime }: { runtime?: TelemetryRuntime } = {}) {
  const view = useVideoTelemetry(true, runtime);
  const t = useT();
  const [collapsed, setCollapsed] = useState(readCollapsed);

  const toggle = () =>
    setCollapsed((current) => {
      writeCollapsed(!current);
      return !current;
    });

  const { metrics, streams } = view.snapshot;
  const mbps = metrics?.receivedMbps ?? 0;
  const fps = metrics?.presentationFps;
  const droppedRatio = metrics?.droppedFrameRatio ?? 0;
  const peak = view.history.reduce((max, p) => Math.max(max, p.receivedMbps), 0);

  const streamCounts = useMemo(() => {
    let main = 0;
    let sub = 0;
    let playing = 0;
    let paused = 0;
    let connecting = 0;
    let error = 0;

    for (const s of streams) {
      if (s.quality === "main") main++;
      else sub++;

      if (s.state === "ACTIVE") playing++;
      else if (s.state === "SUSPENDED" || s.state === "STOPPED") paused++;
      else if (s.state === "CONNECTING" || s.state === "BUFFERING") connecting++;
      else if (s.state === "ERROR") error++;
    }

    if (metrics) {
      main = metrics.mainStreams;
      sub = metrics.subStreams;
    }
    return { main, sub, playing, paused, connecting, error };
  }, [streams, metrics]);

  const [capabilities] = useState(() => {
    const startup = startupProfile.getSnapshot();
    const threads = typeof navigator !== "undefined" ? navigator.hardwareConcurrency : undefined;
    const gpuRaw = startup.capabilities?.gpu.renderer ?? detectQuickGpu();
    return {
      threads,
      gpu: gpuShortName(gpuRaw),
      gpuRaw,
    };
  });

  const stateKey: HealthState = view.state;
  const healthLabel = HEALTH_KEYS[stateKey] ? t(HEALTH_KEYS[stateKey]) : "Óptimo";
  const healthTone = HEALTH_TONES[stateKey] ?? "neutral";

  return (
    <section
      aria-label="Telemetría de video"
      data-testid="video-telemetry-meter"
      className="shrink-0 rounded-m3-lg bg-surface-2 p-2 text-[11px] text-on-surface-variant"
    >
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={toggle}
          aria-expanded={!collapsed}
          aria-label={collapsed ? "Expandir telemetría de video" : "Contraer telemetría de video"}
          className="grid size-5 shrink-0 place-items-center rounded-m3-sm hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-primary"
        >
          <Icon icon={collapsed ? ChevronUp : ChevronDown} size="xs" />
        </button>
        <span className="min-w-0 flex-1 text-base font-semibold tabular-nums text-on-surface" data-testid="telemetry-mbps">
          {mbps.toFixed(1)} <span className="text-[11px] font-medium text-on-surface-variant">Mbps</span>
        </span>
        <Pill tone={healthTone} size="sm" dot>
          {healthLabel}
        </Pill>
      </div>
      {!collapsed && (
        <div className="mt-1.5 flex flex-col gap-1.5">
          <Histogram history={view.history} peak={peak} />
          <dl className="grid grid-cols-3 gap-x-2 gap-y-1">
            <Stat label="FPS" value={fps === undefined ? "—" : fps.toFixed(0)} title="Cuadros presentados por segundo" />
            <Stat label="Descartes" value={`${(droppedRatio * 100).toFixed(1)} %`} title="Cuadros descartados" />
            <Stat label="Streams" value={`Main ${streamCounts.main} · Sub ${streamCounts.sub}`} />
          </dl>
          <p className="flex flex-wrap gap-x-2 tabular-nums" aria-label="Estado de los streams">
            <span title="En vivo">▶ {streamCounts.playing} en vivo</span>
            <span title="En pausa">⏸ {streamCounts.paused} en pausa</span>
            <span title="Conectando">⟳ {streamCounts.connecting} conectando</span>
            <span title="Con error">⚠ {streamCounts.error} error</span>
          </p>
          <p className="flex min-w-0 items-center gap-1.5">
            <span className="shrink-0">{capabilities.threads === undefined ? "CPU —" : `CPU ${capabilities.threads} hilos`}</span>
            <span aria-hidden>·</span>
            <span className="min-w-0 truncate" title={capabilities.gpuRaw ?? "GPU no disponible"}>
              {capabilities.gpu ? `GPU ${capabilities.gpu}` : "GPU no disponible"}
            </span>
          </p>
        </div>
      )}
    </section>
  );
}
