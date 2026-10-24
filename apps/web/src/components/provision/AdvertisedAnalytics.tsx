import type { Schemas } from "@/api/client";

type Analytic = Schemas["CameraAnalyticsCapability"];

const labels: Record<string, string> = {
  lpr: "Matrículas (LPR)",
  face: "Rostros",
  person: "Personas",
  vehicle: "Vehículos",
  line_crossing: "Cruce de línea",
  intrusion: "Intrusión en área",
  motion: "Movimiento",
  tamper: "Sabotaje",
};

/**
 * AdvertisedAnalytics lists the analytics a camera says it can report over ONVIF events. They are
 * advertised, not proven: a camera lists a topic whether or not it ever fires it, so the topics stay
 * available as evidence and the text says so.
 */
export function AdvertisedAnalytics({ analytics }: { analytics: Analytic[] | undefined }) {
  const items = analytics ?? [];
  return (
    <div className="flex flex-col gap-2">
      <span className="text-xs font-semibold uppercase text-muted">Analíticas anunciadas por la cámara (ONVIF)</span>
      {items.length === 0 ? (
        <p className="text-xs text-muted">
          La cámara no anunció eventos de analítica por ONVIF. Las lecturas LPR que envía por HTTP POST no aparecen acá.
        </p>
      ) : (
        <>
          <ul className="flex flex-wrap gap-1.5">
            {items.map((a) => (
              <li
                key={a.id}
                title={a.topics.join("\n")}
                className="rounded-m3-sm border border-outline-variant/30 bg-surface-2 px-2 py-1 text-xs text-on-surface-variant"
              >
                {labels[a.id] ?? a.id}
              </li>
            ))}
          </ul>
          <p className="text-xs text-muted">
            Anunciada significa que la cámara declara el evento; no está probada hasta que llega uno real.
          </p>
        </>
      )}
    </div>
  );
}
