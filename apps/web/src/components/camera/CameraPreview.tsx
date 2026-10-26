import { Camera, Maximize2, Minimize2, Pause, Play, Video, Volume2, VolumeX } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Icon } from "@/components/Icon";
import { MsePlayer } from "@/components/MsePlayer";
import { cn } from "@/lib/cn";

type Quality = "sub" | "main";

/**
 * Permanent preview of the editor: a snapshot until the user plays it, then the same live player
 * Live view uses. It is not a Live view tile: no grid, no layout, only the basic controls.
 */
export function CameraPreview({ cameraId, name, serverId, initialQuality = "sub", overlay }: { cameraId: string; name: string; serverId?: string; initialQuality?: Quality; overlay?: ReactNode }) {
  const [playing, setPlaying] = useState(false);
  const [muted, setMuted] = useState(true);
  const [maximized, setMaximized] = useState(false);
  const [quality, setQuality] = useState<Quality>(initialQuality);
  const [imgError, setImgError] = useState(false);
  const snapshotUrl = `/media/v1/cameras/${cameraId}/snapshot.jpg`;
  const btn = "inline-flex size-9 items-center justify-center rounded-full bg-black/60 text-white backdrop-blur hover:bg-black/80";

  return (
    <div className={cn(maximized ? "fixed inset-0 z-50 bg-black p-4" : "w-full shrink-0 lg:w-96")}>
      <div className={cn("relative overflow-hidden rounded-m3-xl border border-outline-variant/80 bg-black", maximized ? "size-full" : "aspect-video")}>
        {playing ? (
          <MsePlayer cameraId={cameraId} quality={quality} serverId={serverId} muted={muted} objectFit="contain" className="size-full" />
        ) : !imgError ? (
          <img src={`${snapshotUrl}?h=360`} alt={`Vista previa de ${name}`} className="size-full object-contain" onError={() => setImgError(true)} />
        ) : (
          <div className="flex size-full flex-col items-center justify-center gap-2 text-muted">
            <Icon icon={Video} size="md" className="opacity-40" />
            <span className="font-mono text-xs">Sin vista previa</span>
          </div>
        )}
        <span className="absolute left-2 top-2 rounded-m3-xs bg-black/60 px-1.5 py-0.5 font-mono text-[10px] font-bold text-white">
          {playing ? "EN VIVO" : "IMAGEN"} · {quality === "main" ? "MAIN" : "SUB"}
        </span>
        {overlay && <div className="absolute right-2 top-2">{overlay}</div>}
        <div role="toolbar" aria-label="Controles de la vista previa" className="absolute inset-x-2 bottom-2 flex items-center justify-between gap-2">
          <div className="flex gap-1.5">
            <button type="button" className={btn} aria-label={playing ? "Pausar" : "Reproducir"} onClick={() => setPlaying((p) => !p)}>
              <Icon icon={playing ? Pause : Play} size="xs" />
            </button>
            <button type="button" className={btn} aria-label={muted ? "Activar sonido" : "Silenciar"} aria-pressed={!muted} onClick={() => setMuted((m) => !m)}>
              <Icon icon={muted ? VolumeX : Volume2} size="xs" />
            </button>
            <a className={btn} aria-label="Capturar imagen" href={snapshotUrl} download={`${name}.jpg`}>
              <Icon icon={Camera} size="xs" />
            </a>
          </div>
          <div className="flex items-center gap-1.5">
            <div role="group" aria-label="Stream" className="flex overflow-hidden rounded-full bg-black/60 font-mono text-[10px] text-white">
              {(["sub", "main"] as const).map((q) => (
                <button key={q} type="button" aria-pressed={quality === q} onClick={() => setQuality(q)} className={cn("px-2.5 py-2", quality === q && "bg-white/25 font-bold")}>
                  {q === "main" ? "Main" : "Sub"}
                </button>
              ))}
            </div>
            <button type="button" className={btn} aria-label={maximized ? "Restaurar" : "Maximizar"} onClick={() => setMaximized((m) => !m)}>
              <Icon icon={maximized ? Minimize2 : Maximize2} size="xs" />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
