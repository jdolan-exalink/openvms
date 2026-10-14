import { X } from "lucide-react";
import { useState } from "react";
import { useVideoTelemetry } from "@/components/clientStation/hooks";
import { HEALTH_KEYS } from "@/components/clientStation/keys";
import { Icon } from "@/components/Icon";
import { useT } from "@/i18n";
import { overlayVisibleOnLive, writeOverlayPreference } from "@/lib/telemetry";

/**
 * Developer metrics strip over the Live grid. Shown only when the operator turned it on in
 * "Estación cliente" (or with the player debug key). While hidden it holds no telemetry
 * runtime, so it costs nothing; while shown it re-renders once per 1 Hz snapshot.
 */
export function TelemetryOverlay() {
  // The preference is read when Live mounts; changing it happens on another page.
  const [shown, setShown] = useState(overlayVisibleOnLive);
  const view = useVideoTelemetry(shown);
  const t = useT();
  if (!shown) return null;
  const m = view.snapshot.metrics;
  const line = m
    ? t("clientStation.overlayLine", {
        streams: m.activeStreams,
        mbps: m.receivedMbps.toFixed(1),
        fps: m.presentationFps === undefined ? "—" : Math.round(m.presentationFps),
        dropped: (m.droppedFrameRatio * 100).toFixed(1),
        main: m.mainStreams,
        sub: m.subStreams,
        state: t(HEALTH_KEYS[view.state]),
      })
    : t("clientStation.overlayWaiting");
  return (
    <div
      role="group"
      aria-label={t("clientStation.overlayLabel")}
      className="pointer-events-none absolute top-2 right-2 z-20 flex max-w-[calc(100%-1rem)] items-start gap-1 rounded-m3-md bg-surface-dim/80 py-1 pr-1 pl-2.5 font-mono text-[11px] text-on-surface tabular-nums backdrop-blur"
    >
      <p className="min-w-0 py-0.5 break-words">{line}</p>
      <button
        type="button"
        aria-label={t("clientStation.overlayClose")}
        title={t("clientStation.overlayClose")}
        onClick={() => {
          writeOverlayPreference(false);
          setShown(false);
        }}
        className="pointer-events-auto flex size-5 shrink-0 items-center justify-center rounded-full hover:bg-on-surface/10 focus-visible:outline-2 focus-visible:outline-primary"
      >
        <Icon icon={X} size="xs" />
      </button>
    </div>
  );
}
