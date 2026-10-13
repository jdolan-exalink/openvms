import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PlayerState } from "@/lib/live/playerState";
import { videoTelemetry } from "@/lib/telemetry/videoTelemetry";
import { TELEMETRY_COLLAPSED_KEY, VideoTelemetryMeter } from "./VideoTelemetryMeter";

function addSession(state: PlayerState, quality: "main" | "sub") {
  return videoTelemetry.register({ cameraId: "c", quality, getState: () => state, video: document.createElement("video") });
}

beforeEach(() => {
  vi.useFakeTimers();
  localStorage.clear();
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("VideoTelemetryMeter", () => {
  it("renders Mbps, health, stream counts and station info, and updates on each tick", () => {
    const live = addSession("ACTIVE", "sub");
    const paused = addSession("SUSPENDED", "main");
    render(<VideoTelemetryMeter />);
    expect(screen.getByTestId("telemetry-mbps")).toHaveTextContent("0.0 Mbps");
    expect(screen.getByText("Óptimo")).toBeInTheDocument();

    live.addBytes(4_800_000); // 38.4 Mbps over 1 s
    act(() => { vi.advanceTimersByTime(1000); });
    expect(screen.getByTestId("telemetry-mbps")).toHaveTextContent("38.4 Mbps");
    expect(screen.getByText(/▶ 1 en vivo/)).toBeInTheDocument();
    expect(screen.getByText(/⏸ 1 en pausa/)).toBeInTheDocument();
    expect(screen.getByText("Main 1 · Sub 1")).toBeInTheDocument();
    expect(screen.getByText(/GPU no disponible|GPU /)).toBeInTheDocument();
    expect(screen.getByText(/^CPU/)).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /Histograma.*38\.4 Mbps/ })).toBeInTheDocument();

    live.unregister();
    paused.unregister();
  });

  it("caps the histogram at 60 bars", () => {
    const live = addSession("ACTIVE", "sub");
    render(<VideoTelemetryMeter />);
    for (let i = 0; i < 70; i++) {
      live.addBytes(125_000);
      act(() => { vi.advanceTimersByTime(1000); });
    }
    expect(screen.getAllByTestId("telemetry-bar")).toHaveLength(60);
    live.unregister();
  });

  it("collapses to Mbps + health and remembers the choice", () => {
    const { unmount } = render(<VideoTelemetryMeter />);
    expect(screen.getByRole("img", { name: /Histograma/ })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Contraer telemetría de video" }));
    expect(screen.queryByRole("img", { name: /Histograma/ })).toBeNull();
    expect(screen.getByTestId("telemetry-mbps")).toBeInTheDocument();
    expect(screen.getByText("Óptimo")).toBeInTheDocument();
    expect(localStorage.getItem(TELEMETRY_COLLAPSED_KEY)).toBe("1");
    unmount();

    render(<VideoTelemetryMeter />);
    expect(screen.queryByRole("img", { name: /Histograma/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Expandir telemetría de video" }));
    expect(screen.getByRole("img", { name: /Histograma/ })).toBeInTheDocument();
  });

  it("works when localStorage throws", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("blocked"); });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("blocked"); });
    render(<VideoTelemetryMeter />);
    fireEvent.click(screen.getByRole("button", { name: "Contraer telemetría de video" }));
    expect(screen.queryByRole("img", { name: /Histograma/ })).toBeNull();
    vi.restoreAllMocks();
  });
});
