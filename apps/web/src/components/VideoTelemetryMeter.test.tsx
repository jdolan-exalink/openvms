import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PlayerState } from "@/lib/live/playerState";
import { createTelemetryRuntime, type StreamStatsSource, type TelemetryRuntime } from "@/lib/telemetry";
import { TELEMETRY_COLLAPSED_KEY, VideoTelemetryMeter } from "./VideoTelemetryMeter";

function createMockSession(opts: { state: PlayerState; quality: "main" | "sub"; cameraId?: string }) {
  let bytes = 0;
  const state = opts.state;
  return {
    source: {
      streamStats: () => ({
        cameraId: opts.cameraId ?? "cam-1",
        quality: opts.quality,
        state,
        bytesReceived: bytes,
        decodedFrames: 30,
        droppedFrames: 0,
      }),
    } as StreamStatsSource,
    addBytes(n: number) {
      bytes += n;
    },
  };
}

let nowTime = 1000;
let tickFn: (() => void) | undefined;
let sessions: ReturnType<typeof createMockSession>[] = [];
let runtime: TelemetryRuntime;

beforeEach(() => {
  localStorage.clear();
  nowTime = 1000;
  tickFn = undefined;
  sessions = [];
  runtime = createTelemetryRuntime({
    now: () => nowTime,
    schedule: (tick) => {
      tickFn = tick;
      return () => {
        tickFn = undefined;
      };
    },
    sessions: () => sessions.map((s) => s.source),
  });
});

afterEach(() => {
  cleanup();
});

describe("VideoTelemetryMeter", () => {
  it("renders Mbps, health, stream counts and station info, and updates on each tick", () => {
    const live = createMockSession({ state: "ACTIVE", quality: "sub" });
    const paused = createMockSession({ state: "SUSPENDED", quality: "main" });
    sessions = [live, paused];

    render(<VideoTelemetryMeter runtime={runtime} />);
    expect(screen.getByTestId("telemetry-mbps")).toHaveTextContent("0.0 Mbps");

    // Establish baseline
    nowTime += 1000;
    act(() => {
      tickFn?.();
    });

    live.addBytes(4_800_000); // 38.4 Mbps over 1 s
    nowTime += 1000;
    act(() => {
      tickFn?.();
    });

    expect(screen.getByTestId("telemetry-mbps")).toHaveTextContent("38.4 Mbps");
    expect(screen.getByText(/▶ 1 en vivo/)).toBeInTheDocument();
    expect(screen.getByText(/⏸ 1 en pausa/)).toBeInTheDocument();
    expect(screen.getByText("Main 1 · Sub 1")).toBeInTheDocument();
    expect(screen.getByText(/GPU no disponible|GPU /)).toBeInTheDocument();
    expect(screen.getByText(/^CPU/)).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /Histograma.*38\.4 Mbps/ })).toBeInTheDocument();
  });

  it("caps the histogram at 60 bars", () => {
    const live = createMockSession({ state: "ACTIVE", quality: "sub" });
    sessions = [live];

    render(<VideoTelemetryMeter runtime={runtime} />);
    for (let i = 0; i < 70; i++) {
      live.addBytes(125_000);
      nowTime += 1000;
      act(() => {
        tickFn?.();
      });
    }
    expect(screen.getAllByTestId("telemetry-bar")).toHaveLength(60);
  });

  it("collapses to Mbps + health and remembers the choice", () => {
    const { unmount } = render(<VideoTelemetryMeter runtime={runtime} />);
    expect(screen.getByRole("img", { name: /Histograma/ })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Contraer telemetría de video" }));
    expect(screen.queryByRole("img", { name: /Histograma/ })).toBeNull();
    expect(screen.getByTestId("telemetry-mbps")).toBeInTheDocument();
    expect(localStorage.getItem(TELEMETRY_COLLAPSED_KEY)).toBe("1");
    unmount();

    render(<VideoTelemetryMeter runtime={runtime} />);
    expect(screen.queryByRole("img", { name: /Histograma/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Expandir telemetría de video" }));
    expect(screen.getByRole("img", { name: /Histograma/ })).toBeInTheDocument();
  });

  it("works when localStorage throws", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    render(<VideoTelemetryMeter runtime={runtime} />);
    fireEvent.click(screen.getByRole("button", { name: "Contraer telemetría de video" }));
    expect(screen.queryByRole("img", { name: /Histograma/ })).toBeNull();
    vi.restoreAllMocks();
  });
});
