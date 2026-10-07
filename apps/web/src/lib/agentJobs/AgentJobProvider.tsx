import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";

export type AgentJobKind = "install" | "update";
export type AgentJobOutcome = "tracking" | "unknown";

export interface AgentJobRegistration {
  kind: AgentJobKind;
  serverId: string;
  serverName: string;
  job: Schemas["ServerAgentInstallJob"];
}

export interface TrackedAgentJob extends AgentJobRegistration {
  outcome: AgentJobOutcome;
  serverObserved: boolean;
  acceptedAt: number;
  changedAt: number;
}

interface AgentJobsContextValue {
  jobs: readonly TrackedAgentJob[];
  registerJob: (registration: AgentJobRegistration) => void;
}

const AgentJobsContext = createContext<AgentJobsContextValue | null>(null);
const maximumPollDurationMs = 11 * 60_000;
const pollRequestTimeoutMs = 15_000;
const maximumTrackedJobs = 128;

const statuses = new Set(["queued", "running", "succeeded", "failed"]);
const stages = new Set(["validating", "connecting", "transferring", "activating", "registering", "complete", "failed"]);
const safeMessages = new Set([
  "Install queued",
  "Validating agent installer",
  "Connecting to the SSH host using stored first-contact key trust",
  "Transferring agent files over SFTP",
  "Activating the agent service",
  "Registering encrypted agent credentials and TLS trust",
  "Agent files installed and credentials registered; HTTPS health is not yet verified",
  "Agent installation failed; remote output was not retained",
  "Agent files may be installed but registration failed; inspect the target before retrying",
  "Agent update and authenticated HTTPS health verified",
  "Confirming existing agent HTTPS trust remains unchanged",
  "Recording verified agent HTTPS trust",
  "SSH host key changed since first use; verify the server identity before retrying.",
  "Could not establish SSH. Verify reachability and root SSH access, then inspect the target before retrying.",
  "The target failed agent-update preflight; inspect OS, architecture, and service state before retrying.",
  "Staging ownership could not be confirmed; inspect the target before retrying.",
  "Agent files could not be transferred; inspect the target before retrying.",
  "Agent activation failed; the previous files were restored.",
  "Agent activation failed and rollback could not be confirmed; inspect the target before retrying.",
  "HTTPS health verification failed; the previous agent files were restored.",
  "HTTPS health verification failed and rollback could not be confirmed; inspect the target before retrying.",
  "Agent health was verified, but update staging cleanup failed; inspect the target before retrying.",
  "Agent HTTPS health succeeded, but trust registration failed; inspect server configuration before retrying.",
  "Agent update failed; inspect the target before retrying.",
]);

function safeSnapshot(job: Schemas["ServerAgentInstallJob"]): Schemas["ServerAgentInstallJob"] {
  if (!validJob(job)) return job;
  if (!job.message || !safeMessages.has(job.message)) return { id: job.id, status: job.status, stage: job.stage };
  return { id: job.id, status: job.status, stage: job.stage, message: job.message };
}

function validJob(job: Schemas["ServerAgentInstallJob"], expectedId?: string): boolean {
  return typeof job.id === "string" && job.id.length > 0 && (!expectedId || job.id === expectedId) &&
    statuses.has(job.status) && stages.has(job.stage);
}

function isActive(job: Schemas["ServerAgentInstallJob"]): boolean {
  return job.status === "queued" || job.status === "running";
}

function jobKey(kind: AgentJobKind, serverId: string, id: string): string {
  return `${kind}:${serverId}:${id}`;
}

function TrackedJobPoller({ entry, onSnapshot, onUnknown }: {
  entry: TrackedAgentJob;
  onSnapshot: (key: string, snapshot: Schemas["ServerAgentInstallJob"]) => void;
  onUnknown: (key: string) => void;
}) {
  const queryClient = useQueryClient();
  const key = jobKey(entry.kind, entry.serverId, entry.job.id);
  const deadline = entry.acceptedAt + maximumPollDurationMs;
  const queryKey = useMemo(() => ["agent-job-center", entry.kind, entry.serverId, entry.job.id] as const,
    [entry.kind, entry.serverId, entry.job.id]);
  const progress = useQuery({
    queryKey,
    enabled: entry.outcome === "tracking" && isActive(entry.job),
    queryFn: async ({ signal }) => {
      const remaining = deadline - Date.now();
      if (remaining <= 0) throw new DOMException("Agent job observation expired", "AbortError");
      const controller = new AbortController();
      const abort = () => controller.abort();
      const timeout = window.setTimeout(abort, Math.min(pollRequestTimeoutMs, remaining));
      signal.addEventListener("abort", abort, { once: true });
      const aborted = new Promise<never>((_resolve, reject) => {
        if (controller.signal.aborted) reject(new DOMException("Agent job poll aborted", "AbortError"));
        else controller.signal.addEventListener("abort", () => reject(new DOMException("Agent job poll aborted", "AbortError")), { once: true });
      });
      try {
        const request = entry.kind === "install"
          ? api.GET("/api/v1/servers/{serverId}/agent/install/{jobId}", {
            params: { path: { serverId: entry.serverId, jobId: entry.job.id } }, signal: controller.signal,
          })
          : api.GET("/api/v1/servers/{serverId}/agent/update-ssh/{jobId}", {
            params: { path: { serverId: entry.serverId, jobId: entry.job.id } }, signal: controller.signal,
          });
        const response = await Promise.race([request, aborted]);
        if (controller.signal.aborted || Date.now() >= deadline) throw new DOMException("Agent job poll expired", "AbortError");
        return unwrap(response);
      } finally {
        window.clearTimeout(timeout);
        signal.removeEventListener("abort", abort);
      }
    },
    initialData: entry.job,
    retry: false,
    refetchInterval: (query) => {
      if (query.state.error || entry.outcome !== "tracking" || !isActive(query.state.data ?? entry.job)) return false;
      if (Date.now() >= deadline) return false;
      return 1_000;
    },
  });

  const snapshot = progress.data;
  const active = isActive(entry.job);
  useEffect(() => {
    if (progress.error) {
      onUnknown(key);
      return;
    }
    if (!progress.isFetched || !snapshot) return;
    if (!validJob(snapshot, entry.job.id)) onUnknown(key);
    else onSnapshot(key, snapshot);
  }, [entry.job.id, key, onSnapshot, onUnknown, progress.error, progress.isFetched, snapshot]);
  useEffect(() => {
    if (entry.outcome !== "tracking" || !active) return;
    const remaining = deadline - Date.now();
    if (remaining <= 0) {
      onUnknown(key);
      return;
    }
    const timeout = window.setTimeout(() => {
      void queryClient.cancelQueries({ queryKey });
      onUnknown(key);
    }, remaining);
    return () => window.clearTimeout(timeout);
  }, [active, deadline, entry.outcome, key, onUnknown, queryClient, queryKey]);

  return null;
}

export function AgentJobProvider({ children }: { children: ReactNode }) {
  const [jobs, setJobs] = useState<TrackedAgentJob[]>([]);

  const registerJob = useCallback((registration: AgentJobRegistration) => {
    if (!validJob(registration.job)) return;
    const now = Date.now();
    setJobs((current) => {
      const key = jobKey(registration.kind, registration.serverId, registration.job.id);
      if (current.some((entry) => jobKey(entry.kind, entry.serverId, entry.job.id) === key)) return current;
      const retained = current.filter((entry) => isActive(entry.job) || now - entry.changedAt < 60 * 60_000);
      if (retained.length >= maximumTrackedJobs) {
        const oldestTerminal = retained.findIndex((entry) => !isActive(entry.job));
        if (oldestTerminal < 0) return current;
        retained.splice(oldestTerminal, 1);
      }
      return [...retained, { ...registration, job: safeSnapshot(registration.job), outcome: "tracking", serverObserved: false, acceptedAt: now, changedAt: now }];
    });
  }, []);

  const onSnapshot = useCallback((id: string, snapshot: Schemas["ServerAgentInstallJob"]) => {
    setJobs((current) => current.map((entry) => {
      if (jobKey(entry.kind, entry.serverId, entry.job.id) !== id || entry.outcome !== "tracking" || !validJob(snapshot, entry.job.id)) return entry;
      return { ...entry, job: safeSnapshot(snapshot), serverObserved: true, changedAt: Date.now() };
    }));
  }, []);

  const onUnknown = useCallback((id: string) => {
    setJobs((current) => current.map((entry) => jobKey(entry.kind, entry.serverId, entry.job.id) === id && entry.outcome === "tracking"
      ? { ...entry, outcome: "unknown", changedAt: Date.now() }
      : entry));
  }, []);

  const value = useMemo(() => ({ jobs, registerJob }), [jobs, registerJob]);
  return <AgentJobsContext.Provider value={value}>
    {children}
    {jobs.map((entry) => <TrackedJobPoller key={jobKey(entry.kind, entry.serverId, entry.job.id)} entry={entry} onSnapshot={onSnapshot} onUnknown={onUnknown} />)}
  </AgentJobsContext.Provider>;
}

export function useAgentJobs(): AgentJobsContextValue {
  const value = useContext(AgentJobsContext);
  if (!value) throw new Error("useAgentJobs must be used inside AgentJobProvider");
  return value;
}
