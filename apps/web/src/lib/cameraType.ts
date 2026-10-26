/** Camera "type" shown in the editor's General section. It is stored as tags only (no column). */
export type CameraType = "generic" | "lpr_frigate" | "lpr_provision";

export const CAMERA_TYPES: { value: CameraType; label: string; hint: string }[] = [
  { value: "generic", label: "Genérica", hint: "Cámara sin lectura de matrículas." },
  { value: "lpr_frigate", label: "LPR · motor de Frigate", hint: "Frigate lee las matrículas con su motor interno (activa LPR en Frigate)." },
  { value: "lpr_provision", label: "LPR · analítica integrada Provision", hint: "La cámara lee las matrículas con su analítica Provision y las envía a nuestro agente." },
];

/** Tags owned by the camera type; every other tag is left alone. */
export const TYPE_TAGS = ["lpr", "provision", "provision_isr"] as const;

const has = (tags: string[] | undefined, tag: string) => !!tags?.some((t) => t.toLowerCase() === tag);

/**
 * cameraTypeOf derives the type from the tags, the Frigate `lpr.enabled` value and whether the
 * device looks like a Provision unit (model/endpoint heuristics).
 */
export function cameraTypeOf(tags: string[] | undefined, opts: { frigateLpr?: boolean; looksProvision?: boolean } = {}): CameraType {
  const lpr = has(tags, "lpr");
  const provisionTag = has(tags, "provision") || has(tags, "provision_isr");
  if (lpr && provisionTag) return "lpr_provision";
  // A Frigate-engine LPR camera can share a Provision model (I6 series): Frigate wins when it is on.
  if (lpr && opts.looksProvision && opts.frigateLpr !== true) return "lpr_provision";
  if (lpr || opts.frigateLpr === true) return "lpr_frigate";
  return "generic";
}

/** tagsForType swaps the type-managed tags and keeps every other tag in its original order. */
export function tagsForType(tags: string[], type: CameraType): string[] {
  const managed = new Set<string>(TYPE_TAGS);
  const rest = tags.filter((t) => !managed.has(t.toLowerCase()));
  if (type === "generic") return rest;
  if (type === "lpr_frigate") return [...rest, "lpr"];
  return [...rest, "provision", "lpr"];
}
