import { HlsPlayer } from "@/components/HlsPlayer";
import { REC_LIMIT_NOTICE } from "@/lib/liveRec";
import type { RecTransport } from "@/lib/useRecPlayback";

export type RecTileState = "player" | "limited" | "denied" | "empty" | "duplicate";

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
        />
      ) : (
        <>
          {(state === "limited" || state === "duplicate") && <img src={`/media/v1/cameras/${cameraId}/snapshot.jpg?h=360`} alt="" draggable={false} className="size-full object-contain opacity-50" />}
          <span role="status" className="absolute inset-0 flex items-center justify-center p-2 text-center text-xs text-white/85">
            {message}
          </span>
        </>
      )}
    </div>
  );
}
