import { describe, expect, it } from "vitest";
import { MOTION_LABEL, recorderMotionEvents } from "./recorderMotion";

const span = (start: string, end: string, motion?: number) => ({ start_time: start, end_time: end, motion });

describe("recorderMotionEvents", () => {
  it("makes a detection of every stretch recorded because of motion, and of no other", () => {
    const events = recorderMotionEvents("cam-1", [
      span("2026-10-28T10:27:41Z", "2026-10-28T10:28:18Z", 1),
      span("2026-10-28T10:31:22Z", "2026-10-28T10:31:57Z", 0),
      span("2026-10-28T10:40:00Z", "2026-10-28T10:40:30Z"),
      span("2026-10-28T11:00:00Z", "2026-10-28T11:00:40Z", 3),
    ]);
    expect(events.map((e) => e.start_time)).toEqual(["2026-10-28T10:27:41Z", "2026-10-28T11:00:00Z"]);
    expect(events[0]).toEqual({
      id: "motion:cam-1:2026-10-28T10:27:41Z",
      camera_id: "cam-1",
      start_time: "2026-10-28T10:27:41Z",
      end_time: "2026-10-28T10:28:18Z",
      severity: "info",
      labels: [MOTION_LABEL],
    });
  });

  it("gives each detection a stable id that tells cameras apart", () => {
    const a = recorderMotionEvents("cam-a", [span("2026-10-28T10:00:00Z", "2026-10-28T10:00:30Z", 1)]);
    const b = recorderMotionEvents("cam-b", [span("2026-10-28T10:00:00Z", "2026-10-28T10:00:30Z", 1)]);
    expect(a[0]!.id).not.toBe(b[0]!.id);
    expect(recorderMotionEvents("cam-a", [span("2026-10-28T10:00:00Z", "2026-10-28T10:00:30Z", 1)])[0]!.id).toBe(a[0]!.id);
  });

  it("is empty for no spans", () => {
    expect(recorderMotionEvents("c", undefined)).toEqual([]);
    expect(recorderMotionEvents("c", [])).toEqual([]);
  });
});
