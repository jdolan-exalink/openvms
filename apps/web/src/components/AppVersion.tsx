import { useEffect, useState } from "react";

const RELEASE = /^\d+\.\d+\.\d+$/;

/** Reads the OpenVMS release from /version.json (written by web.Dockerfile, never cached). */
export function useAppVersion(): string | null {
  const [version, setVersion] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    (async () => {
      try {
        const res = await fetch("/version.json", { cache: "no-store" });
        if (!res.ok) return;
        const body: unknown = await res.json();
        const v = typeof body === "object" && body !== null ? (body as { version?: unknown }).version : undefined;
        // Dev builds have no release number, and a SPA fallback returns HTML (json() throws): show nothing.
        if (live && typeof v === "string" && RELEASE.test(v)) setVersion(v);
      } catch {
        // Missing or malformed metadata is not an error for the user.
      }
    })();
    return () => {
      live = false;
    };
  }, []);
  return version;
}

/** Small muted "OpenVMS vX.Y.Z" label; renders nothing until a release version is known. */
export function AppVersion({ className = "" }: { className?: string }) {
  const version = useAppVersion();
  if (!version) return null;
  return <p className={`text-xs text-on-surface-variant ${className}`}>{`OpenVMS v${version}`}</p>;
}
