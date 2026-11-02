import type { Schemas } from "@/api/client";
import { cn } from "@/lib/cn";
import { NONE } from "../summary";

type Camera = Schemas["Camera"];
type Tone = "ok" | "warn" | "off";
export type HealthTile = { id: string; label: string; value: string; tone: Tone };

const STATUS: Record<string, { label: string; tone: Tone }> = {
  online: { label: "En línea", tone: "ok" },
  degraded: { label: "Degradada", tone: "warn" },
  offline: { label: "Sin conexión", tone: "off" },
};

/** Health tiles built only from the camera row the screen already loaded; unknown data shows a dash. */
export function healthTiles(camera: Camera | undefined): HealthTile[] {
  if (!camera) return [];
  const status = camera.enabled ? (STATUS[camera.status] ?? { label: "Desconocido", tone: "off" as Tone }) : { label: "Deshabilitada", tone: "off" as Tone };
  const caps = camera.onvif_capabilities;
  return [
    { id: "status", label: "Estado", value: status.label, tone: status.tone },
    { id: "fps", label: "Cuadros por segundo", value: camera.fps ? `${Math.round(camera.fps)} fps` : NONE, tone: camera.fps ? "ok" : "off" },
    { id: "codec", label: "Códec", value: camera.live_codec?.toUpperCase() || camera.hq_codec?.toUpperCase() || NONE, tone: "off" },
    { id: "onvif", label: "ONVIF", value: caps || camera.onvif_endpoint ? (camera.onvif_profile || "Disponible") : "No configurado", tone: caps || camera.onvif_endpoint ? "ok" : "off" },
  ];
}

const TONE: Record<Tone, string> = { ok: "text-ok", warn: "text-warn", off: "text-ink" };

export function HealthTiles({ camera }: { camera: Camera | undefined }) {
  const tiles = healthTiles(camera);
  if (tiles.length === 0) return null;
  return (
    <ul aria-label="Salud de la cámara" className="grid grid-cols-2 gap-2 sm:grid-cols-4">
      {tiles.map((t) => (
        <li key={t.id} className="flex flex-col gap-0.5 rounded-m3-lg border border-outline-variant/50 bg-surface-2 p-3">
          <span className="text-[11px] font-bold uppercase tracking-wider text-muted">{t.label}</span>
          <span className={cn("truncate text-base font-semibold", TONE[t.tone])}>{t.value}</span>
        </li>
      ))}
    </ul>
  );
}
