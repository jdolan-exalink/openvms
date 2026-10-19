import type { useT } from "@/i18n";
import type { Schemas } from "@/api/client";

type Translate = ReturnType<typeof useT>;
export type AgentInstallFailureCode = NonNullable<Schemas["ServerAgentInstallJob"]["code"]>;

const failureKeys = {
  not_root: "servers.agentFailNotRoot",
  unsupported_os: "servers.agentFailUnsupportedOs",
  unsupported_architecture: "servers.agentFailUnsupportedArch",
  agent_files_present: "servers.agentFailAgentFilesPresent",
  systemd_required: "servers.agentFailSystemdRequired",
  agent_already_installed: "servers.agentFailAgentInstalled",
  preflight_failed: "servers.agentFailPreflightFailed",
  transfer_failed: "servers.agentFailTransferFailed",
  activation_failed: "servers.agentFailActivationFailed",
  registration_failed: "servers.agentFailRegistrationFailed",
  ssh_auth_failed: "servers.agentFailSshAuthFailed",
  ssh_connection_failed: "servers.agentFailSshConnectFailed",
} as const satisfies Record<AgentInstallFailureCode, string>;

export function isAgentInstallFailureCode(value: unknown): value is AgentInstallFailureCode {
  return typeof value === "string" && Object.prototype.hasOwnProperty.call(failureKeys, value);
}

const versionPattern = /^\d{1,9}\.\d{1,9}\.\d{1,9}$/;
export const versionStates = ["up_to_date", "outdated", "unknown"] as const;

/** A version string is shown only when it is a plain MAJOR.MINOR.PATCH; remote text never reaches the UI. */
export function safeAgentVersion(value: unknown): string | undefined {
  return typeof value === "string" && versionPattern.test(value) ? value : undefined;
}

function installedAgentText(job: Schemas["ServerAgentInstallJob"], t: Translate): string {
  const installed = safeAgentVersion(job.installed_version);
  const bundled = safeAgentVersion(job.bundled_version);
  if (!bundled) return t("servers.agentFailAgentInstalled");
  let head = installed
    ? t("servers.agentFailInstalledHead", { installed, bundled })
    : t("servers.agentFailInstalledHeadUnknown", { bundled });
  if (installed && job.version_state === "up_to_date") head += `: ${t("servers.agentVersionUpToDate")}`;
  if (installed && job.version_state === "outdated") head += `: ${t("servers.agentVersionOutdated")}`;
  const tail = job.registered_here
    ? t("servers.agentFailInstalledHere", { action: t("servers.agentUpdate") })
    : t("servers.agentFailInstalledElsewhere");
  return `${head}. ${tail}`;
}

/** Localized reason for a failed install job, or undefined when the job carries no known code. */
export function agentInstallFailureText(job: Schemas["ServerAgentInstallJob"] | undefined, t: Translate): string | undefined {
  if (!job || !isAgentInstallFailureCode(job.code)) return undefined;
  if (job.code === "agent_already_installed") return installedAgentText(job, t);
  return t(failureKeys[job.code]);
}
