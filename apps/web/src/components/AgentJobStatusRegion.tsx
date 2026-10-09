import { useT } from "@/i18n";
import { useAgentJobs, type TrackedAgentJob } from "@/lib/agentJobs/AgentJobProvider";

function operationLabel(entry: TrackedAgentJob, t: ReturnType<typeof useT>) {
  return entry.kind === "install" ? t("servers.agentJobInstallOperation") : t("servers.agentJobUpdateOperation");
}

function stageLabel(stage: TrackedAgentJob["job"]["stage"], t: ReturnType<typeof useT>) {
  switch (stage) {
    case "validating": return t("servers.agentJobStageValidating");
    case "connecting": return t("servers.agentJobStageConnecting");
    case "transferring": return t("servers.agentJobStageTransferring");
    case "activating": return t("servers.agentJobStageActivating");
    case "registering": return t("servers.agentJobStageRegistering");
    case "complete": return t("servers.agentJobStageComplete");
    case "failed": return t("servers.agentJobStageFailed");
    default: return t("servers.agentJobStageQueued");
  }
}

function jobIdentity(entry: TrackedAgentJob) {
  return `${entry.kind}:${entry.serverId}:${entry.job.id}`;
}

export function AgentJobStatusRegion() {
  const t = useT();
  const { jobs } = useAgentJobs();
  const active = jobs.filter((entry) => entry.outcome === "tracking" && (entry.job.status === "queued" || entry.job.status === "running"));
  const unknown = jobs.filter((entry) => entry.outcome === "unknown");
  const terminal = jobs.filter((entry) => entry.serverObserved && entry.outcome === "tracking" && (entry.job.status === "succeeded" || entry.job.status === "failed"));

  if (active.length === 0 && unknown.length === 0 && terminal.length === 0) return null;

  return <section aria-label={t("servers.agentJobRegion")} className="mb-4 flex flex-col gap-2">
    {active.map((entry) => <div key={jobIdentity(entry)} role="status" aria-live="polite" className="rounded-m3-lg border border-line bg-surface-1 px-4 py-3 text-sm">
      <span className="font-semibold">{operationLabel(entry, t)}</span>
      <span className="mx-2 text-muted">·</span>
      <span>{entry.serverName}</span>
      <span className="mx-2 text-muted">·</span>
      <span>{stageLabel(entry.job.stage, t)}</span>
    </div>)}
    {unknown.map((entry) => <div key={jobIdentity(entry)} role="status" aria-live="polite" data-agent-job-unknown={jobIdentity(entry)} className="rounded-m3-lg border border-warn/40 bg-warn/10 px-4 py-3 text-sm">
      <span className="font-semibold">{operationLabel(entry, t)}</span>
      <span className="mx-2 text-muted">·</span>
      <span>{entry.serverName}</span>
      <p>{t("servers.agentJobOutcomeUnknown")}</p>
    </div>)}
    {terminal.map((entry) => {
      const succeeded = entry.job.status === "succeeded";
      const message = succeeded
        ? entry.kind === "install" ? t("servers.agentJobInstallSucceeded") : t("servers.agentJobUpdateSucceeded")
        : entry.kind === "install" ? t("servers.agentJobInstallFailed") : t("servers.agentJobUpdateFailed");
      return <div key={jobIdentity(entry)} data-agent-job-notice={jobIdentity(entry)} role={succeeded ? "status" : "alert"} aria-live={succeeded ? "polite" : "assertive"} className={`rounded-m3-lg border px-4 py-3 text-sm ${succeeded ? "border-ok/40 bg-ok/10" : "border-bad/40 bg-bad/10"}`}>
        <span className="font-semibold">{operationLabel(entry, t)}</span>
        <span className="mx-2 text-muted">·</span>
        <span>{entry.serverName}</span>
        <p>{message}</p>
      </div>;
    })}
  </section>;
}
