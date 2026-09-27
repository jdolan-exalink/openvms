import type { Schemas } from "@/api/client";

/**
 * can reports whether the user holds an ALLOW for permission anywhere. It only decides
 * what the UI offers; the API authorizes every request on its own.
 */
export function can(me: Schemas["Me"] | undefined, permission: string): boolean {
  return !!me?.grants.some((g) => g.permission === permission && g.effect === "allow");
}
