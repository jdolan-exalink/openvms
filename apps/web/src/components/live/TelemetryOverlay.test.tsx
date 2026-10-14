import { act, fireEvent, render, screen } from "@testing-library/react";
import { Profiler } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { StreamStatsSample, StreamStatsSource } from "@/lib/telemetry";

const h = vi.hoisted(() => ({ clock: 0, streams: [] as unknown[], tick: undefined as (() => void) | undefined, scheduled: 0 }));

vi.mock("@/lib/telemetry/runtime", async (orig) => {
  const actual = await orig<typeof import("@/lib/telemetry/runtime")>();
  const telemetryRuntime = actual.createTelemetryRuntime({
    now: () => h.clock,
    schedule: (fn) => {
      h.tick = fn;
      h.scheduled += 1;
      return () => {
        h.tick = undefined;
      };
    },
    sessions: () => h.streams as StreamStatsSource[],
  });
  return { ...actual, telemetryRuntime };
});

import { telemetryRuntime } from "@/lib/telemetry/runtime";
import { TelemetryOverlay } from "./TelemetryOverlay";

class FakeStream implements StreamStatsSource {
  bytes = 0;
  decoded = 0;
  dropped = 0;
  constructor(private readonly cameraId: string, private readonly quality: "sub" | "main" = "sub") {}
  streamStats(): StreamStatsSample {
    return { cameraId: this.cameraId, quality: this.quality, state: "ACTIVE", bytesReceived: this.bytes, decodedFrames: this.decoded, droppedFrames: this.dropped, width: 640, height: 360, reconnects: 0, stalls: 0 };
  }
}

function tick(streams: FakeStream[]) {
  h.clock += 1000;
  for (const s of streams) {
    s.bytes += 625_000; // 5 Mbps each
    s.decoded += 100;
    s.dropped += 1;
  }
  act(() => h.tick?.());
}

beforeEach(() => {
  h.clock = 1_000;
  h.streams = [];
  h.scheduled = 0;
  localStorage.clear();
});
afterEach(() => localStorage.clear());

describe("TelemetryOverlay", () => {
  it("renders nothing and never acquires the runtime by default", () => {
    const { container } = render(<TelemetryOverlay />);
    expect(container).toBeEmptyDOMElement();
    expect(telemetryRuntime.consumers()).toBe(0);
    expect(h.scheduled).toBe(0);
  });

  it("appears with the telemetryOverlay key and releases the runtime on unmount", () => {
    localStorage.setItem("openvms.live.telemetryOverlay", "1");
    const { unmount } = render(<TelemetryOverlay />);
    expect(screen.getByRole("group", { name: "Métricas del videowall" })).toBeInTheDocument();
    expect(telemetryRuntime.consumers()).toBe(1);
    unmount();
    expect(telemetryRuntime.consumers()).toBe(0);
  });

  it("also appears with the player debug key", () => {
    localStorage.setItem("openvms.live.debug", "1");
    render(<TelemetryOverlay />);
    expect(screen.getByRole("group", { name: "Métricas del videowall" })).toBeInTheDocument();
  });

  it("ignores other values of the keys", () => {
    localStorage.setItem("openvms.live.telemetryOverlay", "0");
    localStorage.setItem("openvms.live.debug", "true");
    const { container } = render(<TelemetryOverlay />);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the totals of the latest snapshot", () => {
    localStorage.setItem("openvms.live.telemetryOverlay", "1");
    const streams = [new FakeStream("a"), new FakeStream("b"), new FakeStream("c", "main")];
    h.streams = streams;
    render(<TelemetryOverlay />);
    expect(screen.getByText("Midiendo…")).toBeInTheDocument();
    tick(streams);
    const text = screen.getByRole("group", { name: "Métricas del videowall" }).textContent ?? "";
    expect(text).toContain("3 streams");
    expect(text).toContain("15.0 Mbps");
    expect(text).toContain("Descartados 1.0%");
    expect(text).toContain("Main: 1");
    expect(text).toContain("Sub: 2");
    expect(text).toContain("Estado: Sin datos");
  });

  it("re-renders once per tick, never per frame", () => {
    localStorage.setItem("openvms.live.telemetryOverlay", "1");
    const streams = [new FakeStream("a")];
    h.streams = streams;
    let commits = 0;
    render(
      <Profiler id="overlay" onRender={() => (commits += 1)}>
        <TelemetryOverlay />
      </Profiler>,
    );
    const before = commits;
    tick(streams);
    expect(commits - before).toBe(1);
    tick(streams);
    expect(commits - before).toBe(2);
  });

  it("closes from its button, clears the saved preference and stops sampling", () => {
    localStorage.setItem("openvms.live.telemetryOverlay", "1");
    const { container } = render(<TelemetryOverlay />);
    fireEvent.click(screen.getByRole("button", { name: "Cerrar overlay de métricas" }));
    expect(container).toBeEmptyDOMElement();
    expect(localStorage.getItem("openvms.live.telemetryOverlay")).not.toBe("1");
    expect(telemetryRuntime.consumers()).toBe(0);
  });

  it("does not take pointer events away from the grid, except on its close button", () => {
    localStorage.setItem("openvms.live.telemetryOverlay", "1");
    render(<TelemetryOverlay />);
    const panel = screen.getByRole("group", { name: "Métricas del videowall" });
    expect(panel.className).toContain("pointer-events-none");
    expect(screen.getByRole("button", { name: "Cerrar overlay de métricas" }).className).toContain("pointer-events-auto");
  });
});
