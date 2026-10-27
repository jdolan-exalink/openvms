import type { Schemas } from "@/api/client";
import { cn } from "@/lib/cn";

export type SyncState = Schemas["CameraConfigState"]["sync_state"];

export const SYNC_LABEL: Record<SyncState, string> = {
  synced: "Sincronizado",
  pending: "Cambios pendientes",
  drift: "Diferencia detectada",
  error: "Error",
  unverifiable: "No verificable",
};

const TONE: Record<SyncState, string> = {
  synced: "bg-ok/15 text-ok",
  pending: "bg-warn/15 text-warn",
  drift: "bg-warn/15 text-warn",
  error: "bg-bad/12 text-bad",
  unverifiable: "bg-on-surface/8 text-muted",
};

/** Header badge with the OpenVMS-vs-engine sync state of the camera. */
export function SyncStatusBadge({ state }: { state: SyncState }) {
  return (
    <span data-testid="sync-badge" className={cn("inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium", TONE[state])}>
      {SYNC_LABEL[state]}
    </span>
  );
}
