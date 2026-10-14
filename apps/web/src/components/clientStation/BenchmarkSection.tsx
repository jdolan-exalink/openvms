import { Button, Card, ErrorNote, Pill, Table, Th } from "@/components/ui";
import { useT } from "@/i18n";
import { fmtDateTime } from "@/lib/format";
import { summarizeBenchmark, type BenchmarkResult } from "@/lib/telemetry";
import type { BenchmarkView } from "./hooks";
import { HEALTH_KEYS, HEALTH_TONES, REUSE_REASON_KEYS, STOP_REASON_KEYS } from "./keys";

/**
 * "Benchmark de estación": runs only when the operator asks. While it runs, the test tiles are
 * mounted in a visible overlay (they must really be on screen to be decoded and drawn). The
 * overlay element is always in the tree, hidden, so the hook can hand it to the runner.
 */
export function BenchmarkSection({ bench }: { bench: BenchmarkView }) {
  const t = useT();
  const { run, startup, running, canRun, display, start, cancel, attachStage } = bench;
  const progress = run.progress;
  const reasons = !display?.fresh && startup.reuse && !startup.reuse.reusable ? startup.reuse.reasons : [];
  return (
    <Card title={t("clientStation.benchTitle")}>
      <p className="text-sm text-on-surface-variant">{t("clientStation.benchHelp")}</p>
      <p className="text-xs text-on-surface-variant">{t("clientStation.benchScope")}</p>

      <div className="flex flex-wrap items-center gap-3">
        <Button variant="tonal" disabled={!canRun} onClick={start}>
          {t(display ? "clientStation.benchRerun" : "clientStation.benchRun")}
        </Button>
        {!canRun && !running && <span className="text-xs text-on-surface-variant">{t("clientStation.benchWaitCaps")}</span>}
      </div>

      {run.status === "cancelled" && <p role="status" className="text-sm text-on-surface-variant">{t("clientStation.benchCancelled")}</p>}
      {run.status === "error" && <ErrorNote error={new Error(t("clientStation.benchFailed", { error: run.error ?? "" }))} />}
      {run.status === "done" && run.result?.stoppedReason === "hidden" && <p role="status" className="text-sm text-warn">{t("clientStation.benchHidden")}</p>}
      {run.status === "done" && !run.saved && run.result && run.result.stoppedReason !== "hidden" && (
        <p role="status" className="text-xs text-on-surface-variant">{t("clientStation.benchNotSaved")}</p>
      )}

      {display && !(display.fresh && display.result.stoppedReason === "hidden") ? (
        <>
          {!display.fresh && display.record && (
            <div className="flex flex-col gap-1">
              <h3 className="text-sm font-bold">{t("clientStation.benchPreviousTitle")}</h3>
              <p className="text-xs text-on-surface-variant">{t("clientStation.benchPreviousDate", { date: fmtDateTime(new Date(display.record.date)) })}</p>
              {reasons.length > 0 ? (
                <p className="text-sm font-medium text-warn">{t("clientStation.benchRecommend", { reasons: reasons.map((r) => t(REUSE_REASON_KEYS[r])).join(", ") })}</p>
              ) : (
                startup.reuse && <p className="text-xs text-on-surface-variant">{t("clientStation.benchReusable")}</p>
              )}
            </div>
          )}
          <BenchmarkResultView result={display.result} />
        </>
      ) : (
        !running && run.status !== "cancelled" && <p className="text-sm text-on-surface-variant">{t("clientStation.benchNever")}</p>
      )}

      <div
        role={running ? "dialog" : undefined}
        aria-label={running ? t("clientStation.benchDialog") : undefined}
        hidden={!running}
        className={running ? "fixed inset-0 z-[80] flex flex-col items-center justify-center gap-3 bg-scrim p-4" : "hidden"}
      >
        <div className="flex w-full max-w-[968px] flex-col gap-2 rounded-m3-xl bg-surface p-4">
          <p role="status" className="text-base font-bold">
            {progress?.phase === "testing" ? t("clientStation.benchProgress", { streams: progress.streams }) : t("clientStation.benchLoading")}
          </p>
          <p className="text-xs text-on-surface-variant">{t("clientStation.benchStageNote")}</p>
          <div>
            <Button variant="outlined" onClick={cancel}>{t("clientStation.benchCancel")}</Button>
          </div>
        </div>
        <div ref={attachStage} data-testid="benchmark-stage" aria-hidden className="flex max-h-[75vh] w-full max-w-[968px] flex-wrap content-start gap-px overflow-hidden bg-black" />
      </div>
    </Card>
  );
}

function BenchmarkResultView({ result }: { result: BenchmarkResult }) {
  const t = useT();
  const summary = summarizeBenchmark(result);
  const stopKey = STOP_REASON_KEYS[result.stoppedReason];
  const stoppedAt = result.limits.stoppedAtStreams;
  const dash = t("clientStation.unknown");
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-1">
        <h3 className="text-sm font-bold">{t("clientStation.benchResultTitle")}</h3>
        <ul className="flex flex-col gap-0.5 text-sm">
          {summary.rows.map((row) => (
            <li key={row.streams}>{t("clientStation.benchResultLine", { streams: row.streams, state: t(HEALTH_KEYS[row.state]) })}</li>
          ))}
          <li className="text-on-surface-variant">{t("clientStation.benchHq")}</li>
        </ul>
        <p className="text-sm font-medium">
          {summary.maxStableStreams > 0 ? t("clientStation.benchStable", { streams: summary.maxStableStreams }) : t("clientStation.benchNoneStable")}
        </p>
        {stopKey && stoppedAt !== undefined && <p className="text-xs text-on-surface-variant">{t(stopKey, { streams: stoppedAt })}</p>}
      </div>
      <Table label={t("clientStation.benchTable")}>
        <thead>
          <tr>
            <Th>{t("clientStation.benchStreams")}</Th>
            <Th>{t("clientStation.state")}</Th>
            <Th>{t("clientStation.benchDecodedFps")}</Th>
            <Th>{t("clientStation.benchDropped")}</Th>
            <Th>{t("clientStation.benchFrameP95")}</Th>
          </tr>
        </thead>
        <tbody>
          {result.levels.map((l) => (
            <tr key={l.streams}>
              <td>{l.streams}</td>
              <td>
                {l.ran && l.state !== "unknown" ? (
                  <Pill tone={HEALTH_TONES[l.state]} size="sm" dot>{t(HEALTH_KEYS[l.state])}</Pill>
                ) : (
                  <span className="text-on-surface-variant">{l.ran ? t(HEALTH_KEYS.unknown) : t("clientStation.benchNotRun")}</span>
                )}
              </td>
              <td>{l.ran && l.decodedFps !== undefined ? l.decodedFps.toFixed(1) : dash}</td>
              <td>{l.ran && l.droppedRatio !== undefined ? `${(l.droppedRatio * 100).toFixed(1)}%` : dash}</td>
              <td>{l.ran && l.frameTimeP95Ms !== undefined ? `${Math.round(l.frameTimeP95Ms)} ms` : dash}</td>
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
}
