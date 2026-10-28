import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { RecordingTimeline } from "./RecordingTimeline";

afterEach(cleanup);

const DAY0 = Date.parse("2026-10-28T03:00:00Z") / 1000;

describe("RecordingTimeline", () => {
  it("keeps a half-minute stretch visible on a day-wide bar", () => {
    const { container } = render(
      <RecordingTimeline day={DAY0} spans={[{ start_time: "2026-10-28T10:27:41Z", end_time: "2026-10-28T10:28:18Z", motion: 1 }]} events={[]} position={DAY0} onSeek={() => {}} />,
    );
    const bar = container.querySelector("div.bg-accent\\/30") as HTMLElement;
    expect(bar).toBeTruthy();
    expect(bar.className).toContain("min-w-[2px]");
  });

  it("draws a recorder's motion detections as markers named in Spanish", () => {
    render(
      <RecordingTimeline
        day={DAY0}
        spans={[]}
        events={[{ id: "motion:c:1", start_time: "2026-10-28T10:27:41Z", severity: "info", labels: ["motion"] }]}
        position={DAY0}
        onSeek={() => {}}
      />,
    );
    expect(screen.getByTitle(/Movimiento/)).toBeInTheDocument();
  });
});
