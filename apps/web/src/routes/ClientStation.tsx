import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { systemInfoQuery } from "@/api/queries";
import { Sparkline } from "@/components/clientStation/Sparkline";
import { useClientCapabilities, useVideoTelemetry } from "@/components/clientStation/hooks";
import { HEALTH_KEYS, HEALTH_TONES, LAYER_KEYS, LAYER_ORDER, REASON_KEYS } from "@/components/clientStation/keys";
import { Button, Card, Empty, ErrorNote, PageHeader, Pill, Switch, Table, Th } from "@/components/ui";
import { useT } from "@/i18n";
import {
  buildDiagnostics,
  copyDiagnostics,
  formatDiagnosticsText,
  readOverlayPreference,
  writeOverlayPreference,
  type ClientCapabilities,
  type CodecCapability,
  type TelemetryView,
} from "@/lib/telemetry";

const COPIED_MS = 2000;
const CODEC_NAMES: Record<CodecCapability["codec"], string> = { h264: "H.264", hevc: "HEVC", vp8: "VP8", vp9: "VP9", av1: "AV1" };

/**
 * Client station: what this PC can do with the video wall. The default view is for operators
 * (plain language); every technical detail is collapsed. Telemetry runs only while this page is open.
 */
export function ClientStation() {
  const t = useT();
  const caps = useClientCapabilities();
  const view = useVideoTelemetry(true);
  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-6">
      <PageHeader title={t("clientStation.title")} description={t("clientStation.description")} />
      {caps.status === "loading" && <p role="status" className="text-sm text-muted">{t("clientStation.loading")}</p>}
      {caps.status === "error" && <ErrorNote error={new Error(t("clientStation.loadError"))} />}
      {caps.status === "ready" && (
        <>
          <SystemSection caps={caps.capabilities} />
          <CodecsSection caps={caps.capabilities} />
        </>
      )}
      <BandwidthSection view={view} />
      <HealthSection view={view} />
      <DiagnosticsSection caps={caps.capabilities} view={view} />
      <OverlaySection />
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-wrap justify-between gap-x-4 gap-y-0.5 text-sm">
      <dt className="text-on-surface-variant">{label}</dt>
      <dd className="min-w-0 break-words font-medium">{children}</dd>
    </div>
  );
}

function SystemSection({ caps }: { caps: ClientCapabilities }) {
  const t = useT();
  const { system, gpu, memory } = caps;
  const cpuCount = caps.cpu.logicalProcessors;
  const browser = [system.browser, system.browserVersion].filter(Boolean).join(" ");
  const memoryValue =
    memory.reportedGB === undefined
      ? undefined
      : t(memory.bucketed ? "clientStation.memoryAtLeast" : "clientStation.memoryExact", { gb: memory.reportedGB });
  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Card title={t("clientStation.systemTitle")}>
        <dl className="flex flex-col gap-2">
          {system.os && <Row label={t("clientStation.os")}>{system.os}</Row>}
          {system.architecture && <Row label={t("clientStation.architecture")}>{system.architecture}</Row>}
          {browser && <Row label={t("clientStation.browser")}>{browser}</Row>}
          {system.screen && (
            <Row label={t("clientStation.screen")}>
              {t("clientStation.screenValue", { width: system.screen.width, height: system.screen.height, dpr: system.screen.devicePixelRatio })}
            </Row>
          )}
          {system.multipleScreens === true && <Row label={t("clientStation.screen")}>{t("clientStation.multipleScreens")}</Row>}
          {system.viewport && <Row label={t("clientStation.viewport")}>{t("clientStation.viewportValue", system.viewport)}</Row>}
        </dl>
        {system.userAgent && (
          <details className="text-xs text-on-surface-variant">
            <summary className="cursor-pointer">{t("clientStation.technicalDetails")}</summary>
            <p className="mt-1 break-all">
              <span className="sr-only">{t("clientStation.userAgent")}: </span>
              <span>{system.userAgent}</span>
            </p>
          </details>
        )}
      </Card>
      <div className="flex flex-col gap-4">
        <Card title={t("clientStation.cpuTitle")}>
          <p className="text-sm font-medium">{cpuCount === undefined ? t("clientStation.unavailable") : t(cpuCount === 1 ? "clientStation.cpuLogicalOne" : "clientStation.cpuLogicalMany", { count: cpuCount })}</p>
          <p className="text-xs text-on-surface-variant">{t("clientStation.cpuNote")}</p>
        </Card>
        <Card title={t("clientStation.memoryTitle")}>
          <p className="text-sm font-medium">{memoryValue ? t("clientStation.memoryReported", { value: memoryValue }) : t("clientStation.memoryUnavailable")}</p>
        </Card>
      </div>
      <Card title={t("clientStation.gpuTitle")} className="md:col-span-2">
        <dl className="flex flex-col gap-2">
          <Row label={t("clientStation.webgl")}>{t(gpu.webgl ? "clientStation.yes" : "clientStation.no")}</Row>
          <Row label={t("clientStation.webgl2")}>{t(gpu.webgl2 ? "clientStation.yes" : "clientStation.no")}</Row>
          {gpu.vendor && <Row label={t("clientStation.gpuVendor")}>{gpu.vendor}</Row>}
          {gpu.renderer && <Row label={t("clientStation.gpuRenderer")}>{gpu.renderer}</Row>}
          {gpu.maxTextureSize !== undefined && <Row label={t("clientStation.gpuMaxTexture")}>{String(gpu.maxTextureSize)}</Row>}
          {gpu.maxViewportDims && <Row label={t("clientStation.gpuMaxViewport")}>{`${gpu.maxViewportDims[0]}×${gpu.maxViewportDims[1]}`}</Row>}
        </dl>
        {!gpu.webgl && <p className="text-xs text-on-surface-variant">{t("clientStation.gpuAbsent")}</p>}
      </Card>
    </div>
  );
}

function CodecsSection({ caps }: { caps: ClientCapabilities }) {
  const t = useT();
  const yesNo = (v: boolean | undefined) => (v === undefined ? t("clientStation.unknown") : t(v ? "clientStation.yes" : "clientStation.no"));
  const sources = new Set(caps.codecs.map((c) => c.source));
  return (
    <Card title={t("clientStation.codecsTitle")}>
      <p className="text-xs text-on-surface-variant">{t("clientStation.codecsHelp")}</p>
      {caps.codecs.length === 0 ? (
        <Empty>{t("clientStation.codecsEmpty")}</Empty>
      ) : (
        <Table label={t("clientStation.codecsTable")}>
          <thead>
            <tr>
              <Th>{t("clientStation.codec")}</Th>
              <Th>{t("clientStation.profile")}</Th>
              <Th>{t("clientStation.supported")}</Th>
              <Th>{t("clientStation.smooth")}</Th>
              <Th>{t("clientStation.efficient")}</Th>
            </tr>
          </thead>
          <tbody>
            {caps.codecs.map((c) => (
              <tr key={`${c.codec}-${c.profile.label}`}>
                <td>{CODEC_NAMES[c.codec]}</td>
                <td>{c.profile.label}</td>
                <td>{yesNo(c.supported)}</td>
                <td>{yesNo(c.smooth)}</td>
                <td>{yesNo(c.powerEfficient)}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {sources.has("isTypeSupported") && <p className="text-xs text-on-surface-variant">{t("clientStation.sourceIsTypeSupported")}</p>}
      {sources.has("unavailable") && <p className="text-xs text-on-surface-variant">{t("clientStation.sourceUnavailable")}</p>}
    </Card>
  );
}

function BandwidthSection({ view }: { view: TelemetryView }) {
  const t = useT();
  const { metrics, streams } = view.snapshot;
  const rows = useMemo(() => [...streams].sort((a, b) => b.bitrateBps - a.bitrateBps), [streams]);
  const mbps = view.history.map((m) => m.receivedMbps);
  const dropped = view.history.map((m) => m.droppedFrames);
  const fmt = (n: number) => n.toFixed(1);
  let summary = "";
  if (mbps.length > 0) {
    summary = t("clientStation.chartSummary", { seconds: mbps.length, min: fmt(Math.min(...mbps)), max: fmt(Math.max(...mbps)), last: fmt(mbps[mbps.length - 1]!) });
    if (dropped.some((d) => d > 0)) summary += t("clientStation.chartSummaryDropped", { dropped: dropped[dropped.length - 1]! });
  }
  return (
    <Card title={t("clientStation.bandwidthTitle")}>
      {!metrics || streams.length === 0 ? (
        <Empty>{t("clientStation.bandwidthEmpty")}</Empty>
      ) : (
        <>
          <p className="text-2xl font-bold tabular-nums">{t("clientStation.bandwidthTotal", { mbps: fmt(metrics.receivedMbps) })}</p>
          <p className="text-xs text-on-surface-variant">{t("clientStation.bandwidthScope")}</p>
          <p className="text-xs text-on-surface-variant tabular-nums">{t("clientStation.streamsSummary", { streams: metrics.activeStreams, main: metrics.mainStreams, sub: metrics.subStreams })}</p>
          <Sparkline values={mbps} secondary={dropped} label={summary} />
          <p className="flex gap-4 text-xs text-on-surface-variant" aria-hidden>
            <span className="text-primary">{t("clientStation.chartLegendMbps")}</span>
            {dropped.some((d) => d > 0) && <span className="text-warn">{t("clientStation.chartLegendDropped")}</span>}
          </p>
          <Table label={t("clientStation.cameraTable")}>
            <thead>
              <tr>
                <Th>{t("clientStation.camera")}</Th>
                <Th>{t("clientStation.stream")}</Th>
                <Th>{t("clientStation.resolution")}</Th>
                <Th>{t("clientStation.fps")}</Th>
                <Th>{t("clientStation.bitrate")}</Th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={`${r.cameraId}:${r.quality}`}>
                  <td className="font-mono text-xs">{r.cameraId}</td>
                  <td>{t(r.quality === "main" ? "clientStation.streamMain" : "clientStation.streamSub")}</td>
                  <td>{r.width && r.height ? `${r.width}×${r.height}` : t("clientStation.unknown")}</td>
                  <td>{r.fps === undefined ? t("clientStation.unknown") : r.fps.toFixed(0)}</td>
                  <td>{`${(r.bitrateBps / 1_000_000).toFixed(1)} Mbps`}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        </>
      )}
    </Card>
  );
}

function HealthSection({ view }: { view: TelemetryView }) {
  const t = useT();
  const { health, state, bottleneck } = view;
  return (
    <Card title={t("clientStation.healthTitle")}>
      <div className="flex flex-wrap items-center gap-2" data-testid="health-state">
        <span className="text-sm text-on-surface-variant">{t("clientStation.healthOverall")}</span>
        <Pill tone={HEALTH_TONES[state]} dot>{t(HEALTH_KEYS[state])}</Pill>
      </div>
      {state === "unknown" ? (
        <p className="text-sm text-on-surface-variant">{t("clientStation.healthWaiting")}</p>
      ) : (
        <>
          <Table label={t("clientStation.layersTable")}>
            <thead>
              <tr>
                <Th>{t("clientStation.layer")}</Th>
                <Th>{t("clientStation.state")}</Th>
              </tr>
            </thead>
            <tbody>
              {LAYER_ORDER.map((layer) => (
                <tr key={layer}>
                  <td>{t(LAYER_KEYS[layer])}</td>
                  <td><Pill tone={HEALTH_TONES[health.layers[layer]]} size="sm" dot>{t(HEALTH_KEYS[health.layers[layer]])}</Pill></td>
                </tr>
              ))}
            </tbody>
          </Table>
          <p className="text-sm font-medium">
            {[
              bottleneck.layer === "none" ? t("clientStation.bottleneckNone") : t("clientStation.bottleneck", { layer: t(LAYER_KEYS[bottleneck.layer]) }),
              bottleneck.estimated ? t("clientStation.estimate") : "",
            ].filter(Boolean).join(" ")}
          </p>
          {health.reasons.length > 0 && (
            <div>
              <h3 className="text-sm font-bold">{t("clientStation.reasons")}</h3>
              <ul className="list-disc pl-5 text-sm text-on-surface-variant">
                {health.reasons.map((r) => (
                  <li key={`${r.layer}:${r.code}`}>{`${t(REASON_KEYS[r.code])} (${t(LAYER_KEYS[r.layer])})`}</li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
    </Card>
  );
}

function DiagnosticsSection({ caps, view }: { caps: ClientCapabilities | undefined; view: TelemetryView }) {
  const t = useT();
  // The system info endpoint is public, so the version is available to every operator.
  const info = useQuery(systemInfoQuery);
  const [copy, setCopy] = useState<"idle" | "copied" | "failed">("idle");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  // Render must stay pure: the report is stamped with the latest tick, or the time the page opened.
  const [openedAt] = useState(() => Date.now());
  useEffect(() => () => clearTimeout(timer.current), []);
  const text = useMemo(
    () =>
      formatDiagnosticsText(
        buildDiagnostics({
          appVersion: info.data?.version,
          commit: info.data?.commit,
          capabilities: caps,
          telemetry: view.snapshot,
          history: view.history,
          health: view.health,
          bottleneck: view.bottleneck,
          generatedAt: view.snapshot.metrics?.timestamp ?? openedAt,
        }),
      ),
    [info.data?.version, info.data?.commit, caps, view, openedAt],
  );
  const onCopy = async () => {
    const ok = await copyDiagnostics(text);
    setCopy(ok ? "copied" : "failed");
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setCopy("idle"), COPIED_MS);
  };
  return (
    <Card title={t("clientStation.advancedTitle")}>
      <p className="text-xs text-on-surface-variant">{t("clientStation.advancedHelp")}</p>
      <div>
        <Button variant="tonal" onClick={() => void onCopy()}>{copy === "copied" ? t("clientStation.copied") : t("clientStation.copy")}</Button>
      </div>
      {copy === "failed" && <ErrorNote error={new Error(t("clientStation.copyFailed"))} />}
      <details>
        <summary className="cursor-pointer text-sm font-medium">{t("clientStation.technicalDetails")}</summary>
        <pre className="mt-2 max-h-96 overflow-auto rounded-m3-md bg-surface-2 p-3 text-xs whitespace-pre-wrap">{text}</pre>
      </details>
    </Card>
  );
}

function OverlaySection() {
  const t = useT();
  const [on, setOn] = useState(readOverlayPreference);
  return (
    <Card title={t("clientStation.overlayTitle")}>
      <p className="text-xs text-on-surface-variant">{t("clientStation.overlayHelp")}</p>
      <Switch
        checked={on}
        label={t("clientStation.overlayToggle")}
        onChange={(next) => {
          setOn(next);
          writeOverlayPreference(next);
        }}
      />
    </Card>
  );
}
