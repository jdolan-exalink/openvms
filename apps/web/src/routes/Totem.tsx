import { useParams } from "@tanstack/react-router";
import { DetectionChannel } from "@/components/visionai/DetectionChannel";
import type { ChannelKind } from "@/lib/detectionChannels";

const KINDS: readonly ChannelKind[] = ["lpr", "faces"];

/**
 * Fullscreen kiosk for an LPR or face detection channel (/totem/lpr, /totem/faces): no app
 * navigation, just the same channel the Live grid shows, sized for reading at a distance.
 */
export function Totem() {
  const { kind } = useParams({ strict: false }) as { kind?: string };
  const channel = KINDS.find((k) => k === kind);
  return (
    <main data-testid="totem" className="fixed inset-0 flex bg-surface-dim text-lg">
      {channel ? (
        <DetectionChannel kind={channel} className="text-base" />
      ) : (
        <div className="m-auto flex flex-col items-center gap-2 text-center text-muted">
          <span className="text-xl font-semibold text-ink">Tótem desconocido</span>
          <span className="text-sm">Usá /totem/lpr o /totem/faces.</span>
        </div>
      )}
    </main>
  );
}
