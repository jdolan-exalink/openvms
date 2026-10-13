import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PlayerSession } from "../live/PlayerSession";
import { createPlayerMetrics } from "../live/playerMetrics";
import { VideoTelemetryService } from "./videoTelemetry";

class FakeSocket {
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  binaryType = "blob";
  send() {}
  close() {}
}
class FakeMediaSource {
  static isTypeSupported() {
    return true;
  }
  addEventListener() {}
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("MediaSource", FakeMediaSource as unknown as typeof MediaSource);
  vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:fake");
  vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("PlayerSession telemetry", () => {
  it("reports received media bytes to the service and unregisters on close", () => {
    const telemetry = new VideoTelemetryService();
    const sockets: FakeSocket[] = [];
    const session = new PlayerSession({
      cameraId: "cam-1",
      quality: "main",
      metrics: createPlayerMetrics(),
      telemetry,
      createSocket: () => {
        const s = new FakeSocket();
        sockets.push(s);
        return s as unknown as WebSocket;
      },
    });
    telemetry.subscribe(() => {});
    session.connect();
    sockets[0]!.onopen?.();
    sockets[0]!.onmessage?.({ data: JSON.stringify({ type: "mse", value: "x" }) });
    sockets[0]!.onmessage?.({ data: new ArrayBuffer(125_000) });
    sockets[0]!.onmessage?.({ data: new ArrayBuffer(125_000) });
    vi.advanceTimersByTime(1000);
    const snap = telemetry.getSnapshot();
    expect(snap.mbps).toBeCloseTo(2, 5);
    expect(snap.streams.main).toBe(1);
    expect(snap.streams.playing).toBe(1);
    session.close();
    vi.advanceTimersByTime(1000);
    expect(telemetry.getSnapshot().streams.total).toBe(0);
  });
});
