import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Schemas } from "@/api/client";
import { serverAgentQuery } from "@/api/queries";
import { formatBytes } from "@/components/servers/storageRetention";
import { AgentUpdateButton } from "@/components/servers/AgentUpdateButton";
import { Button, StatusBadge } from "@/components/ui";
import { type MessageKey, useT } from "@/i18n";
import { isAtLeastAgentVersion, isNewerAgentVersion } from "@/lib/agentVersion";
import { cn } from "@/lib/cn";
import { fmtDateTime } from "@/lib/format";

type Translate = ReturnType<typeof useT>;

export function problemText(problem: Schemas["ServerProblem"], t: Translate) {
  switch (problem.code) {
    case "cameras_offline":
      return t("servers.problemCamerasOffline", { cameras: (problem.cameras ?? []).join(", ") });
    case "storage_high":
      return t("servers.problemStorageHigh", { percent: String(problem.used_percent ?? "") });
    case "recorder_disk_bad":
      return t("servers.problemRecorderDisk");
    default:
      return t("servers.problemDetectionStalled");
  }
}

export function isAgentBinaryCurrent(agent: Schemas["ServerAgent"]) {
  return hasCurrentDigest(agent) || isCurrentByVersion(agent);
}

// An agent reached over the plain HTTP listener exposes no binary identity (no /v1/health), so the
// version it reports decides instead, never the bare status label.
function isCurrentByVersion(agent: Schemas["ServerAgent"]) {
  return agent.installed && agent.binary_status === "current" && agent.binary_outdated === false &&
    !agent.binary_observed?.sha256 && isAtLeastAgentVersion(agent.version, agent.binary_available?.version);
}

function hasCurrentDigest(agent: Schemas["ServerAgent"]) {
  const observed = agent.binary_observed?.sha256;
  const available = agent.binary_available?.sha256;
  const observedArchitecture = agent.binary_observed?.architecture;
  const availableArchitecture = agent.binary_available?.architecture;
  const supportedArchitecture = (architecture: string | undefined) => architecture === "amd64" || architecture === "arm64";
  return agent.installed && agent.binary_status === "current" && agent.binary_outdated === false &&
    !!observed && !!available && /^[a-f0-9]{64}$/i.test(observed) &&
    (observed.toLowerCase() === available.toLowerCase() || isNewerAgentVersion(agent.binary_observed?.version, agent.binary_available?.version)) &&
    supportedArchitecture(observedArchitecture) && supportedArchitecture(availableArchitecture) && observedArchitecture === availableArchitecture;
}

const CAPABILITIES: { key: keyof Schemas["Capabilities"]; label: MessageKey }[] = [
  { key: "review", label: "servers.capReview" },
  { key: "preview", label: "servers.capPreview" },
  { key: "exports", label: "servers.capExports" },
  { key: "lpr", label: "servers.capLpr" },
  { key: "face_recognition", label: "servers.capFaceRecognition" },
  { key: "semantic_search", label: "servers.capSemanticSearch" },
  { key: "audio", label: "servers.capAudio" },
  { key: "ptz", label: "servers.capPtz" },
];

const AGENT_STATUS: Record<Schemas["AgentStatus"], MessageKey> = {
  unknown: "servers.agentStatusUnknown",
  online: "servers.agentStatusOnline",
  degraded: "servers.agentStatusDegraded",
  offline: "servers.agentStatusOffline",
  maintenance: "servers.agentStatusMaintenance",
};

function formatUptime(seconds: number) {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return [days ? `${days}d` : "", days || hours ? `${hours}h` : "", `${minutes}m`].filter(Boolean).join(" ");
}

function Card({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="flex flex-col gap-3 rounded-m3-xl bg-surface-2 p-4">
      <h3 className="text-sm font-bold">{title}</h3>
      {children}
    </section>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 text-sm">
      <dt className="text-xs text-muted">{label}</dt>
      <dd className="text-right">{children}</dd>
    </div>
  );
}

/**
 * ServerSummaryTab is the read-only overview of a server: Frigate, cameras, storage, agent and
 * capabilities. It reads the agent through the same query key as the server row, so both share one
 * cache entry. backup is the original-config backup state; callers that cannot read it omit it and
 * the line is not rendered.
 */
export function ServerSummaryTab({
  server,
  backup,
  onEditCameras,
}: {
  server: Schemas["Server"];
  backup?: { present: boolean; createdAt?: string | null };
  onEditCameras: () => void;
}) {
  const t = useT();
  const agentQuery = useQuery({ ...serverAgentQuery(server.id), refetchInterval: 5000 });
  const agent = agentQuery.data;

  const totalBytes = agent?.cctv_total_bytes ?? (server.storage && server.storage.total_mb > 0 ? server.storage.total_mb * 1024 * 1024 : 0);
  const freeBytes = agent?.cctv_free_bytes ?? (server.storage && server.storage.free_mb > 0 ? server.storage.free_mb * 1024 * 1024 : 0);
  const usedBytes = Math.max(0, totalBytes - freeBytes);

  const currentVersion = agent?.binary_observed?.version || agent?.version || "—";
  const newVersion = agent?.binary_available?.version;
  const versionTone = !agent ? "muted" : isAgentBinaryCurrent(agent) ? "ok" : agent.binary_status === "update_available" || agent.binary_outdated === true ? "warn" : "muted";

  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Card title={t("servers.summaryFrigate")}>
        <dl className="flex flex-col gap-2">
          <Row label={t("servers.summaryStatus")}>
            <StatusBadge status={server.status} />
          </Row>
          <Row label={t("servers.summaryVersion")}>
            <span className="font-mono text-xs">{server.frigate_version || "—"}</span>
          </Row>
          <Row label={t("servers.summaryLastSeen")}>{fmtDateTime(server.last_seen_at)}</Row>
          <Row label={t("servers.summaryUptime")}>
            {server.uptime_seconds != null ? <span className="tabular-nums">{formatUptime(server.uptime_seconds)}</span> : "—"}
          </Row>
        </dl>
        {server.problems?.length ? (
          <ul aria-label={t("servers.problemsLabel")} className="flex flex-col gap-0.5 text-xs break-words text-warn">
            {server.problems.map((problem) => (
              <li key={problem.code}>{problemText(problem, t)}</li>
            ))}
          </ul>
        ) : server.last_error ? (
          <div role="alert" className="text-xs break-words text-bad">{server.last_error}</div>
        ) : null}
      </Card>

      <Card title={t("servers.summaryCameras")}>
        <p className="text-2xl font-bold tabular-nums">{t("servers.summaryCameraCount", { count: server.camera_count })}</p>
        <div>
          <Button size="sm" variant="tonal" onClick={onEditCameras}>
            {t("servers.editCameras")}
          </Button>
        </div>
      </Card>

      <Card title={t("servers.summaryStorage")}>
        {totalBytes > 0 ? (
          <dl className="flex flex-col gap-2">
            <Row label={t("servers.storageUsed")}><span className="font-mono text-xs">{formatBytes(usedBytes)}</span></Row>
            <Row label={t("servers.storageFree")}><span className="font-mono text-xs">{formatBytes(freeBytes)}</span></Row>
            <Row label={t("servers.storageTotal")}><span className="font-mono text-xs">{formatBytes(totalBytes)}</span></Row>
          </dl>
        ) : (
          <p className="text-xs text-muted">{t("servers.storageUnknown")}</p>
        )}
      </Card>

      <Card title={t("servers.summaryAgent")}>
        {agentQuery.isPending ? (
          <p className="text-xs text-muted">{t("servers.agent")}…</p>
        ) : !agent?.installed ? (
          <p className="text-sm text-muted">{agentQuery.isError ? t("servers.agentLifecycleUnknown") : t("servers.agentMissing")}</p>
        ) : (
          <dl className="flex flex-col gap-2">
            <Row label={t("servers.summaryVersion")}>
              <span className="font-mono text-xs">
                <span
                  data-version-tone={versionTone}
                  className={cn(versionTone === "ok" && "font-semibold text-ok", versionTone === "warn" && "font-semibold text-warn", versionTone === "muted" && "text-muted")}
                >
                  {currentVersion}
                </span>
                {versionTone === "warn" && newVersion && newVersion !== currentVersion && (
                  <>
                    <span aria-hidden> → </span>
                    <span className="rounded bg-warn/15 px-1 py-0.5 text-warn">{newVersion}</span>
                  </>
                )}
              </span>
            </Row>
            <Row label={t("servers.agentReportedStatus")}>{t(AGENT_STATUS[server.agent_status ?? "unknown"])}</Row>
            <Row label={t("servers.agentLastHeartbeat")}>{fmtDateTime(server.agent_last_seen_at)}</Row>
            <Row label={t("servers.agentCertExpires")}>{fmtDateTime(server.agent_cert_expires_at)}</Row>
            {backup && (
              <Row label={t("servers.agentBackupLabel")}>
                {backup.present ? `${t("servers.agentBackupPresent")}${backup.createdAt ? ` · ${fmtDateTime(backup.createdAt)}` : ""}` : t("servers.agentBackupMissing")}
              </Row>
            )}
            {agent?.installed && agent.binary_status === "update_available" && (
              <div className="flex items-center justify-between border-t border-outline/20 pt-2">
                <span className="text-xs font-medium text-warn">{t("servers.agentUpdateAvailableBanner")}</span>
                <AgentUpdateButton server={server} />
              </div>
            )}
          </dl>
        )}
      </Card>

      <div className="md:col-span-2">
        <Card title={t("servers.summaryCapabilities")}>
          <ul aria-label={t("servers.summaryCapabilities")} className="flex flex-wrap gap-2">
            {CAPABILITIES.map(({ key, label }) => {
              const enabled = server.capabilities?.[key] === true;
              return (
                <li
                  key={key}
                  data-enabled={enabled}
                  title={enabled ? undefined : t("servers.capabilityDisabled")}
                  className={cn(
                    "rounded-full px-3 py-1 text-xs font-medium",
                    enabled ? "bg-primary-container text-on-primary-container" : "bg-surface-3 text-muted line-through",
                  )}
                >
                  {t(label)}
                </li>
              );
            })}
          </ul>
        </Card>
      </div>
    </div>
  );
}
