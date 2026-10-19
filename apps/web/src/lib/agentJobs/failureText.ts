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
  ssh_auth_failed: "servers.agentFailSshAuthFailed",
  ssh_connection_failed: "servers.agentFailSshConnectFailed",
} as const satisfies Record<AgentInstallFailureCode, string>;

export function isAgentInstallFailureCode(value: unknown): value is AgentInstallFailureCode {
  return typeof value === "string" && Object.prototype.hasOwnProperty.call(failureKeys, value);
}

/** Localized reason for a failed install job, or undefined when the job carries no known code. */
export function agentInstallFailureText(job: Pick<Schemas["ServerAgentInstallJob"], "code"> | undefined, t: Translate): string | undefined {
  if (!job || !isAgentInstallFailureCode(job.code)) return undefined;
  return t(failureKeys[job.code]);
}
