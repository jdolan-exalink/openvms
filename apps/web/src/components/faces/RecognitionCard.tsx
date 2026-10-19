import type { Schemas } from "@/api/client";
import { LinkButton } from "@/components/ui";
import { useT } from "@/i18n";
import { fmtDateTime } from "@/lib/format";

/**
 * RecognitionCard is one sighting: the event picture (when the caller may see it), the camera,
 * the time and the link to the recording. The row stays without the picture; the name is shown
 * only where the card is not already inside the person's own page.
 */
export function RecognitionCard({ r, showPerson = true, showImage = true }: { r: Schemas["FaceRecognition"]; showPerson?: boolean; showImage?: boolean }) {
  const t = useT();
  return (
    <li className="flex gap-3 rounded-m3-xl bg-surface-1 p-3">
      {showImage &&
        (r.has_thumbnail ? (
          <img src={`/api/v1/events/${r.event_id}/thumbnail`} alt={t("faces.recThumbAlt", { camera: r.camera_name })} loading="lazy" className="size-24 shrink-0 rounded-m3-lg bg-surface-2 object-cover" />
        ) : (
          <div className="size-24 shrink-0 rounded-m3-lg bg-surface-2" aria-hidden />
        ))}
      <div className="flex min-w-0 flex-col gap-1 text-sm">
        {showPerson && <span className="truncate font-bold">{r.person_name}</span>}
        <span className="truncate">{r.camera_name}</span>
        <span className="truncate text-xs text-muted">{r.site_name} · {fmtDateTime(r.start_time)}</span>
        <LinkButton to="/playback" search={{ camera: r.camera_id, t: Math.floor(new Date(r.start_time).getTime() / 1000) }} variant="text" size="sm" className="w-fit">
          {t("faces.viewPlayback")}
        </LinkButton>
      </div>
    </li>
  );
}
