import type { Schemas } from "@/api/client";
import { cn } from "@/lib/cn";
import { fmtDateTime } from "@/lib/format";
import { useT } from "@/i18n";

type Reception = Schemas["AnalyticsReception"];

const DOT: Record<Reception["state"], string> = {
  receiving: "bg-ok",
  idle: "bg-warn",
  down: "bg-bad",
};

/**
 * ReceptionLed shows whether the analytics a Provision camera or recorder reports are reaching central.
 * The state is written out next to the dot so it never depends on color alone, and the title carries the
 * time of the last one. A camera that has nothing to do with Provision has no state and renders nothing.
 */
export function ReceptionLed({ reception }: { reception: Reception | undefined }) {
  const t = useT();
  if (!reception) return null;
  const label = { receiving: t("servers.receptionReceiving"), idle: t("servers.receptionIdle"), down: t("servers.receptionDown") }[reception.state];
  const title = reception.last_event_at ? t("servers.receptionLastEvent", { time: fmtDateTime(reception.last_event_at) }) : t("servers.receptionNoEvents");
  return (
    <span role="status" data-state={reception.state} title={title} className="inline-flex items-center gap-1.5 text-xs text-on-surface-variant">
      <span className={cn("size-2 shrink-0 rounded-full", DOT[reception.state])} aria-hidden />
      {label}
    </span>
  );
}
