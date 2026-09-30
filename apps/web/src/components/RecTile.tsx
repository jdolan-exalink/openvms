import { useState } from "react";
import { HlsPlayer } from "@/components/HlsPlayer";
import { REC_LIMIT_NOTICE } from "@/lib/liveRec";
import type { RecTransport } from "@/lib/useRecPlayback";

export type RecTileState = "player" | "limited" | "denied" | "empty" | "duplicate";

const FROZEN_MAX_W = 640;

/** grabFrame copies the current picture of a video into a small JPEG data URL (undefined when it has none). */
function grabFrame(video: HTMLVideoElement | null | undefined): string | undefined {
  if (!video || video.readyState < 2 || !video.videoWidth) return undefined;
  try {
    const scale = Math.min(1, FROZEN_MAX_W / video.videoWidth);
    const canvas = document.createElement("canvas");
    canvas.width = Math.round(video.videoWidth * scale);
    canvas.height = Math.round(video.videoHeight * scale);
    canvas.getContext("2d")?.drawImage(video, 0, 0, canvas.width, canvas.height);
    return canvas.toDataURL("image/jpeg", 0.7);
  } catch {
    return undefined;
  }
}

/**
 * RecTile is the recorded-playback layer of a Live grid tile in REC mode. It covers the live
 * player (kept suspended underneath) and shows either the synchronized HLS player or the
 * reason there is none: over the player cap, no recordings permission, or no recording that day.
 */
export function RecTile({
  cameraId,
  name,
  state,
  transport,
  isMaster,
}: {
  cameraId: string;
  name: string;
  state: RecTileState;
  transport: RecTransport;
  isMaster: boolean;
}) {
  const posterHere = transport.poster?.cameraId === cameraId ? transport.poster : null;
  // A window change remounts the player: freeze its last picture (still attached during this render)
  // so the tile keeps showing it instead of flashing black until the new window has a frame.
  const [seenStart, setSeenStart] = useState(transport.win.start);
  const [frozen, setFrozen] = useState<string>();
  if (seenStart !== transport.win.start) {
    setSeenStart(transport.win.start);
    if (state === "player") setFrozen(grabFrame(transport.players.current.get(cameraId)?.video));
  }
  const message = state === "denied" ? "Sin permiso de grabaciones" : state === "empty" ? "Sin grabaciones este día" : state === "limited" ? REC_LIMIT_NOTICE : state === "duplicate" ? "Ya visible en otra celda" : "";
  return (
    <div className="absolute inset-0 z-[2] bg-black" data-rec-tile={cameraId}>
      {state === "player" ? (
        <HlsPlayer
          ref={transport.bindPlayer(cameraId)}
          key={`${cameraId}-${transport.win.start}`}
          cameraId={cameraId}
          start={transport.win.start}
          end={transport.win.end}
          startOffset={transport.win.offset}
          controls={false}
          muted
          autoPlay={transport.playing}
          rate={transport.speed}
          onTime={isMaster ? transport.onMasterTime : undefined}
          ariaLabel={`Reproducción grabada de ${name}`}
          className="size-full"
          poster={posterHere ? `/media/v1/events/${posterHere.eventId}/snapshot.jpg` : frozen}
          posterFallback={posterHere ? `/api/v1/events/${posterHere.eventId}/thumbnail` : undefined}
          posterKey={posterHere?.key}
          posterChip={!!posterHere}
        />
      ) : (
        <>
          {posterHere && state === "empty" && (
            <img
              src={`/media/v1/events/${posterHere.eventId}/snapshot.jpg`}
              onError={(e) => {
                const fallback = `/api/v1/events/${posterHere.eventId}/thumbnail`;
                if (!e.currentTarget.src.endsWith(fallback)) e.currentTarget.src = fallback;
              }}
              alt=""
              draggable={false}
              className="size-full object-contain"
            />
          )}
          {(state === "limited" || state === "duplicate") && <img src={`/media/v1/cameras/${cameraId}/snapshot.jpg?h=360`} alt="" draggable={false} className="size-full object-contain opacity-50" />}
          <span role="status" className="absolute inset-0 flex items-center justify-center p-2 text-center text-xs text-white/85">
            {message}
          </span>
        </>
      )}
    </div>
  );
}
