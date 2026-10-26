// Mirrors internal/provision.agentVersionIsNewer: only MAJOR.MINOR.PATCH versions are compared.
const semver = /^v?(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$/;

function parts(version: string | undefined): [number, number, number] | null {
  const match = version ? semver.exec(version) : null;
  return match ? [Number(match[1]), Number(match[2]), Number(match[3])] : null;
}

export function isNewerAgentVersion(version: string | undefined, other: string | undefined): boolean {
  const a = parts(version);
  const b = parts(other);
  if (!a || !b) return false;
  if (a[0] !== b[0]) return a[0] > b[0];
  if (a[1] !== b[1]) return a[1] > b[1];
  return a[2] > b[2];
}

// isAtLeastAgentVersion is true when both are MAJOR.MINOR.PATCH and version is not older than other.
export function isAtLeastAgentVersion(version: string | undefined, other: string | undefined): boolean {
  const a = parts(version);
  const b = parts(other);
  if (!a || !b) return false;
  return !isNewerAgentVersion(other, version);
}
