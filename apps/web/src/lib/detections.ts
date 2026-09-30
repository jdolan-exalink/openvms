import { labelName } from "@/lib/format";

/** Coarse detection families used to color the timeline. */
export type DetectionCategory = "person" | "vehicle" | "plate" | "animal" | "other";

const vehicles = new Set(["car", "motorcycle", "bicycle", "truck", "bus", "van", "vehicle"]);
const animals = new Set(["dog", "cat", "bird", "horse", "cow", "sheep", "bear", "animal"]);

export function detectionCategory(label: string): DetectionCategory {
  if (label === "person" || label === "face") return "person";
  if (label === "license_plate") return "plate";
  if (vehicles.has(label)) return "vehicle";
  if (animals.has(label)) return "animal";
  return "other";
}

/** Subtle, legible in light and dark; deliberately distinct from the accent used for coverage. */
export const CATEGORY_COLOR: Record<DetectionCategory, string> = {
  person: "#f59e0b",
  vehicle: "#a855f7",
  plate: "#06b6d4",
  animal: "#ec4899",
  other: "#94a3b8",
};

/** Spanish title of a detection: "Persona", "Auto", "Patente ABC123" when a plate or sub label is known. */
export function detectionTitle(label: string, detail?: string): string {
  const name = labelName(label) || "Evento";
  if (!detail) return name;
  return label === "license_plate" ? `${name} ${detail}` : `${name} · ${detail}`;
}
