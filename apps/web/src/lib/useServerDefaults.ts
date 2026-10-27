import { useQuery } from "@tanstack/react-query";
import { serverFrigateRawQuery } from "@/api/queries";
import { yamlScalar } from "./frigateGroups";

/** Server-level Frigate defaults a camera inherits; a field is present only when the camera does not override it. */
export type ServerDefaults = { motionThreshold?: string; minScore?: string; minArea?: string; recordRetention?: string };

const RETENTION_PATHS: { path: string[]; label: string }[] = [
  { path: ["record", "retain", "days"], label: "días" },
  { path: ["record", "continuous", "days"], label: "días de grabación continua" },
  { path: ["record", "motion", "days"], label: "días de grabación por movimiento" },
];

/** The camera's own value for a path, read from the cameras: block of config.yml. */
const cameraValue = (yaml: string, camera: string, path: string[]) => yamlScalar(yaml, ["cameras", camera, ...path]);

/**
 * Reads the server defaults from config.yml. A default is reported only when the server sets it
 * and the camera block does not override it (an ambiguous camera block hides the hint, never
 * shows a wrong one). Object filters need every tracked label to share one server value.
 */
export function serverDefaultsFromYaml(yaml: string, camera: string, track: string[]): ServerDefaults {
  if (!camera || !/^[A-Za-z0-9_-]+$/.test(camera)) return {};
  const out: ServerDefaults = {};
  const inherited = (path: string[]) => {
    const v = yamlScalar(yaml, path);
    return v !== undefined && cameraValue(yaml, camera, path) === undefined ? v : undefined;
  };
  out.motionThreshold = inherited(["motion", "threshold"]);
  for (const [key, field] of [["minScore", "min_score"], ["minArea", "min_area"]] as const) {
    const values = track.map((l) => (/^[A-Za-z0-9_-]+$/.test(l) ? inherited(["objects", "filters", l, field]) : undefined));
    if (values.length > 0 && values.every((v) => v !== undefined && v === values[0])) out[key] = values[0];
  }
  for (const r of RETENTION_PATHS) {
    const v = inherited(r.path);
    if (v !== undefined) {
      out.recordRetention = `${v} ${r.label}`;
      break;
    }
  }
  return out;
}

/**
 * Server defaults for the camera editor. Reuses the server config.yml query of the server editor;
 * it is only read when the camera document says the caller may see secrets (the raw endpoint needs it).
 */
export function useServerDefaults({ serverId, camera, track, allowed }: { serverId?: string; camera?: string; track: string[]; allowed: boolean }): ServerDefaults {
  const raw = useQuery({ ...serverFrigateRawQuery(serverId), enabled: !!serverId && allowed, retry: false });
  return raw.data?.yaml && camera ? serverDefaultsFromYaml(raw.data.yaml, camera, track) : {};
}
