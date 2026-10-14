import { ChevronDown, ChevronUp } from "lucide-react";
import { useState } from "react";
import { Icon } from "@/components/Icon";
import { Pill, type PillTone } from "@/components/ui";
import { getClientCapabilities, gpuShortName } from "@/lib/telemetry/clientCapabilities";
import { HEALTH_LABELS, type PerformanceHealth } from "@/lib/telemetry/performanceClassifier";
import { useVideoTelemetry } from "@/lib/telemetry/useVideoTelemetry";
import { HISTORY_SIZE, type HistoryPoint, type VideoTelemetryService } from "@/lib/telemetry/videoTelemetry";

export const TELEMETRY_COLLAPSED_KEY = "openvms.live.telemetryMeter.collapsed";

const HEALTH_TONES: Record<PerformanceHealth, PillTone> = { optimal: "ok", acceptable: "info", limit: "warn", saturated: "bad" };

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
    // storage unavailable: the choice just won't persist
  }
}

const CHART_W = 120;
const CHART_H = 28;

/** Plain-SVG bar histogram of the last minute of Mbps (at most HISTORY_SIZE bars). */
function Histogram({ history, peak }: { history: readonly HistoryPoint[]; peak: number }) {
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
        const h = Math.max((p.mbps / scale) * CHART_H, p.mbps > 0 ? 1 : 0);
        return (
          <rect
            key={p.at}
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

/** Compact live-video meter pinned to the bottom of the camera sidebar. Only this component follows the 1 Hz ticks. */
export function VideoTelemetryMeter({ service }: { service?: VideoTelemetryService } = {}) {
  const snap = useVideoTelemetry(service);
  const [collapsed, setCollapsed] = useState(readCollapsed);
  const [caps] = useState(getClientCapabilities);
  const toggle = () =>
    setCollapsed((current) => {
      writeCollapsed(!current);
      return !current;
    });

  const { streams } = snap;
  const peak = snap.history.reduce((max, p) => Math.max(max, p.mbps), 0);
  const gpu = gpuShortName(caps.gpu.renderer);
  const threads = caps.cpu.logicalProcessors;
  const health = HEALTH_LABELS[snap.health];

  return (
    <section aria-label="Telemetría de video" data-testid="video-telemetry-meter" className="shrink-0 rounded-m3-lg bg-surface-2 p-2 text-[11px] text-on-surface-variant">
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
          {snap.mbps.toFixed(1)} <span className="text-[11px] font-medium text-on-surface-variant">Mbps</span>
        </span>
        <Pill tone={HEALTH_TONES[snap.health]} size="sm" dot>
          {health}
        </Pill>
      </div>
      {!collapsed && (
        <div className="mt-1.5 flex flex-col gap-1.5">
          <Histogram history={snap.history} peak={peak} />
          <dl className="grid grid-cols-3 gap-x-2 gap-y-1">
            <Stat label="FPS" value={snap.fps === null ? "—" : snap.fps.toFixed(0)} title="Cuadros presentados por segundo, promedio por stream" />
            <Stat label="Descartes" value={`${(snap.droppedRatio * 100).toFixed(1)} %`} title="Cuadros descartados" />
            <Stat label="Streams" value={`Main ${streams.main} · Sub ${streams.sub}`} />
          </dl>
          <p className="flex flex-wrap gap-x-2 tabular-nums" aria-label="Estado de los streams">
            <span title="En vivo">▶ {streams.playing} en vivo</span>
            <span title="En pausa">⏸ {streams.paused} en pausa</span>
            <span title="Conectando">⟳ {streams.connecting} conectando</span>
            <span title="Con error">⚠ {streams.error} error</span>
          </p>
          <p className="flex min-w-0 items-center gap-1.5">
            <span className="shrink-0">{threads === null ? "CPU —" : `CPU ${threads} hilos`}</span>
            <span aria-hidden>·</span>
            <span className="min-w-0 truncate" title={caps.gpu.renderer ?? "GPU no disponible"}>
              {gpu ? `GPU ${gpu}` : "GPU no disponible"}
            </span>
          </p>
        </div>
      )}
    </section>
  );
}
