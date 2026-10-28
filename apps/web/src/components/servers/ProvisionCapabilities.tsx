import type { Schemas } from "@/api/client";
import { Pill, type PillTone } from "@/components/ui";
import { type MessageKey, useT } from "@/i18n";

type Capability = Schemas["ProvisionNvrCapability"];

const NAMES: Record<string, MessageKey> = {
  web_api: "servers.provCapWebApi",
  channels: "servers.provCapChannels",
  channel_status: "servers.provCapChannelStatus",
  storage_health: "servers.provCapStorageHealth",
  onvif: "servers.provCapOnvif",
  rtsp: "servers.provCapRtsp",
  ptz: "servers.provCapPtz",
  playback: "servers.provCapPlayback",
  events: "servers.provCapEvents",
  analytics: "servers.provCapAnalytics",
  native_api: "servers.provCapNativeApi",
  snmp: "servers.provCapSnmp",
  lpr_match: "servers.provCapLprMatch",
  face_match: "servers.provCapFaceMatch",
};

const STATES: Record<Capability["state"], { label: MessageKey; tone: PillTone }> = {
  confirmed: { label: "servers.capStateConfirmed", tone: "ok" },
  unknown: { label: "servers.capStateUnknown", tone: "warn" },
  unsupported: { label: "servers.capStateUnsupported", tone: "neutral" },
};

/**
 * ProvisionCapabilities shows what a Provision-ISR recorder can do. Only "confirmed" means it was seen
 * working; "unverified" is what the recorder claims or advertises and must not read as available.
 */
export function ProvisionCapabilities({ capabilities }: { capabilities: Capability[] }) {
  const t = useT();
  return (
    <div className="flex flex-col gap-2">
      <p className="text-xs text-muted">{t("servers.provisionCapabilitiesHint")}</p>
      <ul aria-label={t("servers.provisionCapabilitiesTitle")} className="grid gap-2 sm:grid-cols-2">
        {capabilities.map((c) => {
          const state = STATES[c.state];
          return (
            <li key={c.name} className="flex flex-col gap-1 rounded-m3-lg bg-surface-2 p-3 text-sm">
              <div className="flex items-center justify-between gap-2">
                <span className="font-semibold">{NAMES[c.name] ? t(NAMES[c.name]!) : c.name}</span>
                <Pill tone={state.tone} size="sm">{t(state.label)}</Pill>
              </div>
              {c.detail && <span className="text-xs break-words text-muted">{c.detail}</span>}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
