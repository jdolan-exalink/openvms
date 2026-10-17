import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { persistedToEditorValue, planManagedZoneChanges, splitFrigateZones } from "./cameraZones";

const zone = (over: Partial<Schemas["CameraZone"]> = {}): Schemas["CameraZone"] => ({
  id: "z1",
  camera_id: "c1",
  name: "entrada",
  points: [{ x: 0.1, y: 0.1 }, { x: 0.9, y: 0.1 }, { x: 0.5, y: 0.9 }],
  settings: { inertia: 3 },
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
  sync_status: "synced",
  ...over,
});

describe("persisted camera zones in the editor", () => {
  it("shows persisted zones keyed by name with their stable IDs", () => {
    const value = persistedToEditorValue([zone()]);
    expect(value.zones).toEqual({ entrada: { inertia: 3, coordinates: "0.1,0.1,0.9,0.1,0.5,0.9" } });
    expect(value.managedZones).toEqual({ entrada: { id: "z1" } });
  });

  it("hides OpenVMS-owned remote keys from the foreign zones and reports them as preserved", () => {
    const { foreign, preserved } = splitFrigateZones({ patio: { coordinates: "0,0,1,0,1,1" }, ovms_entrada: { coordinates: "0.1,0.1,0.9,0.1,0.5,0.9" } });
    expect(foreign).toEqual({ patio: { coordinates: "0,0,1,0,1,1" } });
    expect(preserved).toEqual({ ovms_entrada: { coordinates: "0.1,0.1,0.9,0.1,0.5,0.9" } });
  });
});

describe("planManagedZoneChanges", () => {
  const coords = "0.1,0.1,0.9,0.1,0.5,0.9";

  it("does nothing when nothing changed, even if points differ below persisted precision", () => {
    const plan = planManagedZoneChanges([zone()], { zones: { entrada: { inertia: 3, coordinates: "0.1001,0.1,0.9,0.1,0.5,0.9" } }, managedZones: { entrada: { id: "z1" } } });
    expect(plan).toEqual({ create: [], update: [], remove: [] });
  });

  it("creates zones without an ID, updates changed ones keeping the ID on rename, and removes the rest", () => {
    const plan = planManagedZoneChanges([zone(), zone({ id: "z2", name: "borrar" })], {
      zones: {
        entrada_norte: { inertia: 5, coordinates: coords },
        nueva: { coordinates: "0.2,0.2,0.8,0.2,0.5,0.7" },
      },
      managedZones: { entrada_norte: { id: "z1" }, nueva: {} },
    });
    expect(plan.remove).toEqual(["z2"]);
    expect(plan.update).toEqual([
      { id: "z1", input: { name: "entrada_norte", points: [{ x: 0.1, y: 0.1 }, { x: 0.9, y: 0.1 }, { x: 0.5, y: 0.9 }], settings: { inertia: 5 } } },
    ]);
    expect(plan.create).toEqual([{ name: "nueva", points: [{ x: 0.2, y: 0.2 }, { x: 0.8, y: 0.2 }, { x: 0.5, y: 0.7 }], settings: {} }]);
  });
});
