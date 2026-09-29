import { useState } from "react";
import type { SessionSnapshot } from "@/lib/live/PlayerSession";

/** Where the gateway serves the latest frame of a camera (used until a live frame was captured). */
export function coldSnapshotUrl(cameraId: string): string {
  return `/media/v1/cameras/${cameraId}/snapshot.jpg?h=360`;
}

/**
 * PlayerStatusOverlay is what a live tile shows while its video is not playing: the last frame
 * captured in memory or, cold, the camera snapshot, with the connection state on top. A spinner
 * only appears when there is no image at all. Stopped errors (session expired, no permission,
 * unsupported codec) offer a "Reintentar" button; retryable ones keep reconnecting by themselves.
 */
export function PlayerStatusOverlay({
  cameraId,
  snapshot,
  onRetry,
}: {
  cameraId: string;
  snapshot: SessionSnapshot;
  onRetry?: () => void;
}) {
  const [coldState, setColdState] = useState<"loading" | "ready" | "failed">("loading");
  const image = snapshot.poster ?? (coldState !== "failed" ? coldSnapshotUrl(cameraId) : null);
  const hasImage = snapshot.poster !== null || coldState === "ready";
  const { error } = snapshot;
  const stopped = snapshot.state === "ERROR";
  const text = error?.label ?? snapshot.message ?? "";
  const caption = text || (snapshot.state === "SUSPENDED" ? "En pausa" : "Conectando…");
  return (
    <div className="absolute inset-0 z-[2]" data-testid="player-status">
      {image && (
        <img
          src={image}
          alt=""
          draggable={false}
          className="absolute inset-0 size-full object-contain"
          onLoad={() => setColdState("ready")}
          onError={() => snapshot.poster === null && setColdState("failed")}
        />
      )}
      <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 bg-black/40 p-2 text-center text-xs text-white">
        {!hasImage && !stopped && (
          <span role="progressbar" aria-label="Conectando" className="size-5 animate-spin rounded-full border-2 border-white/30 border-t-white" />
        )}
        <span role={stopped ? "alert" : "status"} className="rounded bg-black/60 px-2 py-0.5">
          {caption}
        </span>
        {stopped && onRetry && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onRetry();
            }}
            onPointerDown={(e) => e.stopPropagation()}
            className="rounded border border-white/60 bg-black/60 px-2 py-1 font-medium hover:bg-white/20 focus-visible:outline focus-visible:outline-2 focus-visible:outline-white"
          >
            Reintentar
          </button>
        )}
      </div>
    </div>
  );
}
