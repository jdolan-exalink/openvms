import { appearanceColor, labelName, vehicleColorText, vehiclePaint, vehicleQualification } from "@/lib/format";

type Vehicle = {
  type?: string;
  type_confidence?: number;
  color?: string;
  color_confidence?: number;
  trailer_color?: string;
  trailer_color_confidence?: number;
};

type Job = "pending" | "processing" | "completed" | "failed";

type Person = {
  upper_color?: string;
  lower_color?: string;
};

function Swatch({ color }: { color: string | null }) {
  if (!color) return null;
  return <span className="inline-block size-3 shrink-0 rounded-[3px] border border-black/50" style={{ backgroundColor: color }} aria-hidden />;
}

function percent(confidence?: number, text?: string): string {
  if (text === "Detectando" || text === "No detectado" || confidence == null || confidence <= 0) return "";
  return ` ${Math.round(confidence * 100)}%`;
}

function Reading({ text, paint, confidence }: { text: string; paint: string | null; confidence?: number }) {
  if (text === "Detectando") {
    return (
      <span className="inline-flex items-center gap-1">
        <span className="size-1.5 animate-pulse rounded-full bg-accent" aria-hidden />
        Detectando
      </span>
    );
  }
  return (
    <>
      <Swatch color={paint} />
      {text}{percent(confidence, text)}
    </>
  );
}

function jobPending(job?: Job) {
  return job === "pending" || job === "processing";
}

/** VehicleFacts separates Frigate's detection from OpenVMS classification, paint and clothing. */
export function VehicleFacts({
  labels,
  vehicle,
  person,
  serverName,
  cameraName,
  vehicleJob,
  personJob,
}: {
  labels: string[];
  vehicle?: Vehicle | null;
  person?: Person | null;
  serverName?: string;
  cameraName?: string;
  vehicleJob?: Job;
  personJob?: Job;
}) {
  const detection = labels.map(labelName).filter(Boolean).join(", ") || "—";
  const waiting = jobPending(vehicleJob);
  const qualification = waiting ? "Detectando" : vehicleQualification(labels, vehicle);
  const color = waiting ? "Detectando" : vehicleColorText(labels, vehicle);
  const paint = color && color !== "No detectado" && color !== "Detectando" ? vehiclePaint(vehicle ?? undefined) : null;
  const rig = !waiting && vehicle?.type === "truck_trailer";
  const trailer = rig ? appearanceColor(vehicle?.trailer_color) : null;
  const clothingPending = jobPending(personJob);
  const upper = labels.includes("person") ? appearanceColor(person?.upper_color, clothingPending) : null;
  const lower = labels.includes("person") ? appearanceColor(person?.lower_color, clothingPending) : null;
  return (
    <div className="flex min-w-0 flex-col gap-0.5 text-xs">
      <span className="truncate"><span className="text-muted">Detección: </span>{detection}</span>
      <span className="truncate">
        <span className="text-muted">Clasificación: </span>
        {waiting ? <Reading text="Detectando" paint={null} /> : <>{qualification}{percent(vehicle?.type_confidence, qualification)}</>}
      </span>
      {color && (
        <span className="inline-flex items-center gap-1.5">
          <span className="text-muted">{rig ? "Camión: " : "Color: "}</span>
          <Reading text={color} paint={paint} confidence={vehicle?.color_confidence} />
        </span>
      )}
      {trailer && (
        <span className="inline-flex items-center gap-1.5">
          <span className="text-muted">Acoplado: </span>
          <Reading text={trailer.text} paint={trailer.paint} confidence={vehicle?.trailer_color_confidence} />
        </span>
      )}
      {upper && (
        <span className="inline-flex items-center gap-1.5">
          <span className="text-muted">Arriba: </span>
          <Reading text={upper.text} paint={upper.paint} />
        </span>
      )}
      {lower && (
        <span className="inline-flex items-center gap-1.5">
          <span className="text-muted">Abajo: </span>
          <Reading text={lower.text} paint={lower.paint} />
        </span>
      )}
      {serverName && <span className="truncate"><span className="text-muted">Servidor: </span>{serverName}</span>}
      {cameraName && <span className="truncate"><span className="text-muted">Cámara: </span>{cameraName}</span>}
    </div>
  );
}
