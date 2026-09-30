import { describe, expect, it } from "vitest";
import { assignRecPlayers, moveToDay, parseRecSearch, pickMaster, recSearch, stepEvent } from "./liveRec";

describe("liveRec", () => {
  it("parses and builds the URL state", () => {
    const iso = "2026-09-30T12:00:00.000Z";
    expect(parseRecSearch({ mode: "rec", t: iso })).toEqual({ rec: true, t: Date.parse(iso) / 1000 });
    expect(parseRecSearch({ mode: "rec", t: 1_800_000_000 })).toEqual({ rec: true, t: 1_800_000_000 });
    expect(parseRecSearch({ mode: "rec", t: "garbage" })).toEqual({ rec: true, t: undefined });
    expect(parseRecSearch({ t: iso })).toEqual({ rec: false });
    expect(recSearch(true, Date.parse(iso) / 1000)).toEqual({ mode: "rec", t: iso });
    expect(recSearch(false, 5)).toEqual({ mode: undefined, t: undefined });
  });

  it("caps recorded players and skips cameras that cannot play", () => {
    const ids = ["a", "b", "b", "c", "d"];
    const { players, limited } = assignRecPlayers(ids, (id) => id !== "a", 2);
    expect(players).toEqual(["b", "c"]);
    expect([...limited]).toEqual(["d"]);
  });

  it("picks the selected camera as master when it has coverage, else the first with coverage", () => {
    const players = ["a", "b", "c"];
    expect(pickMaster(players, "b", () => true)).toBe("b");
    expect(pickMaster(players, "b", (id) => id === "c")).toBe("c");
    expect(pickMaster(players, undefined, () => false)).toBe("a");
    expect(pickMaster([], undefined, () => true)).toBe("");
  });

  it("steps between events", () => {
    const times = [10, 20, 30];
    expect(stepEvent(times, 20, 1)).toBe(30);
    expect(stepEvent(times, 20, -1)).toBe(10);
    expect(stepEvent(times, 30, 1)).toBeUndefined();
    expect(stepEvent(times, 10, -1)).toBeUndefined();
  });

  it("keeps the time of day when changing day", () => {
    const at = new Date(2026, 8, 30, 14, 5, 9);
    const moved = moveToDay(Math.floor(at.getTime() / 1000), new Date(2026, 8, 12));
    expect(new Date(moved * 1000)).toEqual(new Date(2026, 8, 12, 14, 5, 9));
    expect(moveToDay(Math.floor(at.getTime() / 1000), new Date(2026, 8, 12), 5)).toBe(5);
  });
});
