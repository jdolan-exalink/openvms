import { useState } from "react";
import type { VisionAISlot } from "@/lib/visionAi";
import { ObjectIcon } from "@/lib/visionAiIcons";

/** Detection image of an active event: snapshot when allowed, else thumbnail, else the object icon. */
export function EventSnapshot({ slot, snapshots }: { slot: VisionAISlot; snapshots: boolean }) {
  const eventId = slot.event_id;
  if (!eventId) return null;
  // Keyed by permission too: if the thumbnail failed before permissions loaded, the snapshot still gets
  // its chance once snapshots.view is known.
  return <EventSnapshotImage key={`${eventId}:${snapshots}`} slot={slot} eventId={eventId} snapshots={snapshots} />;
}

function EventSnapshotImage({ slot, eventId, snapshots }: { slot: VisionAISlot; eventId: string; snapshots: boolean }) {
  const thumbnail = `/api/v1/events/${eventId}/thumbnail`;
  // Snapshot first (when allowed), the thumbnail once on error, then the icon placeholder.
  const [stage, setStage] = useState<"primary" | "thumbnail" | "none">("primary");
  const useSnapshot = snapshots && stage === "primary";
  const src = stage === "none" ? null : useSnapshot ? `/media/v1/events/${eventId}/snapshot.jpg` : thumbnail;
  return (
    <div className="aspect-video w-full overflow-hidden rounded-m3-sm bg-surface-3">
      {src ? (
        <img
          src={src}
          alt={`Detección: ${slot.object ?? "evento"} en ${slot.camera_name ?? "cámara"}`}
          loading="lazy"
          className="size-full object-cover"
          onError={() => setStage(useSnapshot ? "thumbnail" : "none")}
        />
      ) : (
        <div data-testid={`vision-event-placeholder-${eventId}`} className="flex size-full items-center justify-center text-muted">
          <ObjectIcon object={slot.object} className="size-6 opacity-50" />
        </div>
      )}
    </div>
  );
}
