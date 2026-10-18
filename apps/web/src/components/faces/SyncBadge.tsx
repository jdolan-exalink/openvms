import { StatusBadge, type Tone } from "@/components/ui";
import { useT, type MessageKey } from "@/i18n";

const styles: Record<string, { label: MessageKey; tone: Tone }> = {
  synced: { label: "faces.syncSynced", tone: "ok" },
  pending: { label: "faces.syncPending", tone: "info" },
  processing: { label: "faces.syncProcessing", tone: "info" },
  failed: { label: "faces.syncFailed", tone: "bad" },
  conflict: { label: "faces.syncConflict", tone: "warn" },
  disabled: { label: "faces.syncDisabled", tone: "neutral" },
};

/** SyncBadge shows the state of one person on one server. */
export function SyncBadge({ status }: { status: string }) {
  const t = useT();
  const style = styles[status] ?? styles.pending!;
  return <StatusBadge status={status} tone={style.tone} label={t(style.label)} />;
}
