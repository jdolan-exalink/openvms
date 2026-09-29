import { describe, expect, it } from "vitest";
import { emptyEventsForm, formToSearch, parseEventsSearch, searchToForm } from "./eventsSearch";

describe("eventsSearch", () => {
  it("omits defaults so an empty form yields a clean URL", () => {
    expect(formToSearch(emptyEventsForm)).toEqual({});
  });

  it("round-trips every filter through the URL shape", () => {
    const form = {
      ...emptyEventsForm,
      site: "s1",
      camera: "c1",
      cameraGroup: "g1",
      label: "car",
      zone: "entrada",
      subLabel: "placa",
      severity: "alert" as const,
      plate: "AB123CD",
      from: "2024-01-01T10:00",
      to: "2024-01-02T10:00",
      pending: true,
      hasSnapshot: true,
      hasPreview: true,
    };
    const search = formToSearch(form);
    expect(searchToForm(parseEventsSearch(search))).toEqual(form);
  });

  it("coerces router-parsed scalars back to the expected types and drops junk", () => {
    // TanStack Router JSON-parses search values, so a numeric plate arrives as a number.
    const s = parseEventsSearch({ plate: 123456, pending: "true", snapshot: 1, severity: "bogus", zone: { a: 1 }, camera: "" });
    expect(s).toEqual({ plate: "123456", pending: true });
  });
});
