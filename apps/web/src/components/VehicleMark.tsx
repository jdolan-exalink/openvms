import { useT } from "@/i18n";
import { appearanceColor, detectionNames, vehicleColorText, vehiclePaint, vehicleQualification } from "@/lib/format";

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

function percent(confidence: number | undefined, text: string, detecting: string, undetected: string): string {
  if (text === detecting || text === undetected || confidence == null || confidence <= 0) return "";
  return ` ${Math.round(confidence * 100)}%`;
}

function Reading({ text, paint, confidence, detecting, undetected }: { text: string; paint: string | null; confidence?: number; detecting: string; undetected: string }) {
  if (text === detecting) {
    return (
      <span className="inline-flex items-center gap-1">
        <span className="size-1.5 animate-pulse rounded-full bg-accent" aria-hidden />
        {detecting}
      </span>
    );
  }
  return (
    <>
      <Swatch color={paint} />
      {text}{percent(confidence, text, detecting, undetected)}
    </>
  );
}

function jobPending(job?: Job) {
  return job === "pending" || job === "processing";
}

/** VehicleFacts separates Frigate's detection from OpenVMS classification, paint and clothing. */
export function VehicleFacts({
  labels = [],
  vehicle,
  person,
  serverName,
  cameraName,
  vehicleJob,
  personJob,
}: {
  labels?: string[];
  vehicle?: Vehicle | null;
  person?: Person | null;
  serverName?: string;
  cameraName?: string;
  vehicleJob?: Job;
  personJob?: Job;
}) {
  const t = useT();
  const detecting = t("labels.detecting");
  const undetected = t("labels.undetected");
  const tone = { detecting, undetected };
  const detection = detectionNames(labels).join(", ") || "—";
  const waiting = jobPending(vehicleJob);
  const qualification = waiting ? detecting : vehicleQualification(labels, vehicle);
  const color = waiting ? detecting : vehicleColorText(labels, vehicle);
  const paint = color && color !== undetected && color !== detecting ? vehiclePaint(vehicle ?? undefined) : null;
  const rig = !waiting && vehicle?.type === "truck_trailer";
  const trailer = rig ? appearanceColor(vehicle?.trailer_color) : null;
  const clothingPending = jobPending(personJob);
  const upper = labels.includes("person") ? appearanceColor(person?.upper_color, clothingPending) : null;
  const lower = labels.includes("person") ? appearanceColor(person?.lower_color, clothingPending) : null;
  const place = cameraName
    ? t("labels.cameraLine", { name: serverName ? `${cameraName} · ${serverName}` : cameraName })
    : serverName
      ? t("labels.serverLine", { name: serverName })
      : "";
  return (
    <div className="flex min-w-0 flex-col gap-0.5 text-xs leading-4">
      <span className="truncate"><span className="text-muted">{t("labels.factDetection")}: </span>{detection}</span>
      <span className="truncate font-medium text-fg">
        <span className="font-normal text-muted">{t("labels.factClassification")}: </span>
        {waiting ? <Reading text={detecting} paint={null} {...tone} /> : <>{qualification}{percent(vehicle?.type_confidence, qualification, detecting, undetected)}</>}
      </span>
      {(color || trailer) && (
        <span className="flex min-w-0 items-center gap-1.5 truncate">
          {color && (
            <span className="inline-flex min-w-0 items-center gap-1.5">
              <span className="text-muted">{rig ? `${t("labels.factTruck")}: ` : `${t("labels.factColor")}: `}</span>
              <Reading text={color} paint={paint} confidence={vehicle?.color_confidence} {...tone} />
            </span>
          )}
          {trailer && (
            <span className="inline-flex min-w-0 items-center gap-1.5">
              <span className="text-muted">{color ? `· ${t("labels.factTrailer")}:` : `${t("labels.factTrailer")}:`} </span>
              <Reading text={trailer.text} paint={trailer.paint} confidence={vehicle?.trailer_color_confidence} {...tone} />
            </span>
          )}
        </span>
      )}
      {(upper || lower) && (
        <span className="flex min-w-0 items-center gap-1.5 truncate">
          {upper && (
            <span className="inline-flex items-center gap-1.5">
              <span className="text-muted">{t("labels.factUpper")}: </span>
              <Reading text={upper.text} paint={upper.paint} {...tone} />
            </span>
          )}
          {lower && (
            <span className="inline-flex items-center gap-1.5">
              <span className="text-muted">{upper ? `· ${t("labels.factLower")}:` : `${t("labels.factLower")}:`} </span>
              <Reading text={lower.text} paint={lower.paint} {...tone} />
            </span>
          )}
        </span>
      )}
      {place && <span className="truncate text-muted">{place}</span>}
    </div>
  );
}
