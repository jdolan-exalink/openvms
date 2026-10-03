import { describe, expect, it } from "vitest";
import { emptyEventsForm, formToSearch, parseEventsSearch, searchToForm, todayEventsRange } from "./eventsSearch";

describe("eventsSearch", () => {
  it("omits defaults so an empty form yields a clean URL", () => {
    expect(formToSearch(emptyEventsForm)).toEqual({});
  });

  it("defaults a missing range to the current local day and keeps that out of the URL", () => {
    const now = new Date(2026, 9, 3, 15, 4);
    const form = searchToForm({}, now);
    expect(form.from).toBe("2026-10-03T00:00");
    expect(form.to).toBe("2026-10-03T23:59");
    expect(formToSearch(form, now)).toEqual({});
    expect(todayEventsRange(now)).toEqual({ from: "2026-10-03T00:00", to: "2026-10-03T23:59" });
  });

  it("round-trips every filter through the URL shape", () => {
    const form = {
      ...emptyEventsForm,
      server: "srv1",
      camera: "c1",
      label: "car",
      severity: "alert" as const,
      plate: "AB123CD",
      from: "2024-01-01T10:00",
      to: "2024-01-02T10:00",
      pending: true,
      hasSnapshot: true,
      hasPreview: true,
      vehicleType: "suv",
      vehicleColor: "white",
    };
    const search = formToSearch(form);
    expect(searchToForm(parseEventsSearch(search))).toEqual(form);
  });

  it("coerces router-parsed scalars back to the expected types and drops junk", () => {
    const s = parseEventsSearch({ plate: 123456, pending: "true", snapshot: 1, severity: "bogus", camera: "" });
    expect(s).toEqual({ plate: "123456", pending: true });
  });
});
