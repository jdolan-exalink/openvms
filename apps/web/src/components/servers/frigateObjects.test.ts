import { describe, expect, it } from "vitest";
import {
  FRIGATE_SUPPORTED_OBJECTS,
  FRIGATE_OBJECT_CATALOG,
  isFrigateSupportedObject,
  parseTrackedObjects,
  toggleTrackedObject,
} from "./frigateObjects";

describe("frigateObjects catalog and helpers", () => {
  it("includes all primary COCO objects supported by Frigate", () => {
    expect(FRIGATE_SUPPORTED_OBJECTS.has("person")).toBe(true);
    expect(isFrigateSupportedObject("person")).toBe(true);
    expect(isFrigateSupportedObject("car")).toBe(true);
    expect(isFrigateSupportedObject("dog")).toBe(true);
    expect(isFrigateSupportedObject("cat")).toBe(true);
    expect(isFrigateSupportedObject("motorcycle")).toBe(true);
    expect(isFrigateSupportedObject("bicycle")).toBe(true);
    expect(isFrigateSupportedObject("bus")).toBe(true);
    expect(isFrigateSupportedObject("truck")).toBe(true);
    expect(isFrigateSupportedObject("backpack")).toBe(true);
    expect(isFrigateSupportedObject("package")).toBe(true);

    // Unsupported object
    expect(isFrigateSupportedObject("spaceship")).toBe(false);
    expect(isFrigateSupportedObject("laser")).toBe(false);
  });

  it("organizes catalog into clear categories with icons and labels", () => {
    expect(FRIGATE_OBJECT_CATALOG.length).toBeGreaterThan(15);
    const person = FRIGATE_OBJECT_CATALOG.find((o) => o.tag === "person");
    expect(person).toBeDefined();
    expect(person?.category).toBe("people");
    expect(person?.label).toBe("Persona");

    const car = FRIGATE_OBJECT_CATALOG.find((o) => o.tag === "car");
    expect(car).toBeDefined();
    expect(car?.category).toBe("vehicles");
  });

  it("parses tracked objects from YAML text", () => {
    const yaml = `
objects:
  track:
    - person
    - "car"
    - 'dog'
`;
    const tracked = parseTrackedObjects(yaml);
    expect(tracked).toEqual(["person", "car", "dog"]);
  });

  it("returns empty array if no objects section exists", () => {
    const yaml = `mqtt:\n  host: 10.0.0.1\n`;
    expect(parseTrackedObjects(yaml)).toEqual([]);
  });

  it("adds an object to track block when not tracked", () => {
    const yaml = `
objects:
  track:
    - person
`;
    const updated = toggleTrackedObject(yaml, "car");
    expect(parseTrackedObjects(updated)).toEqual(["person", "car"]);
  });

  it("removes an object from track block when already tracked", () => {
    const yaml = `
objects:
  track:
    - person
    - car
`;
    const updated = toggleTrackedObject(yaml, "person");
    expect(parseTrackedObjects(updated)).toEqual(["car"]);
  });

  it("creates objects and track block if absent in YAML", () => {
    const yaml = `mqtt:\n  host: 10.0.0.1`;
    const updated = toggleTrackedObject(yaml, "person");
    expect(parseTrackedObjects(updated)).toEqual(["person"]);
  });
});
