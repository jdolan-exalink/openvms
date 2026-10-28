import { CATEGORY_COLOR, type DetectionCategory } from "@/lib/detections";

const CATEGORIES: { key: DetectionCategory; name: string }[] = [
  { key: "person", name: "Persona" },
  { key: "vehicle", name: "Vehículo" },
  { key: "plate", name: "Patente" },
  { key: "animal", name: "Animal" },
  { key: "other", name: "Movimiento y otros" },
];

/** TimelineLegend is the memory aid under the REC timeline: what each bar, line and detection colour means. */
export function TimelineLegend() {
  return (
    <ul aria-label="Referencias de la línea de tiempo" className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-muted">
      <li className="flex items-center gap-1" title="Hay video grabado en ese tramo">
        <span aria-hidden className="inline-block h-2 w-4 rounded-[2px] bg-primary/40" /> Grabado
      </li>
      <li className="flex items-center gap-1" title="Tramos largos de grabación sin cortes: el equipo graba todo el tiempo, no solo con movimiento">
        <span aria-hidden className="inline-block h-0.5 w-4 bg-red-500" /> Grabación continua
      </li>
      {CATEGORIES.map(({ key, name }) => (
        <li key={key} className="flex items-center gap-1" title={`Detección: ${name.toLowerCase()}`}>
          <span aria-hidden className="inline-block size-2 rounded-full" style={{ backgroundColor: CATEGORY_COLOR[key] }} /> {name}
        </li>
      ))}
    </ul>
  );
}
