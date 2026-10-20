import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PlayerSession } from "./PlayerSession";
import { createPlayerMetrics } from "./playerMetrics";
import { ServerBackoff } from "./serverBackoff";
import { createSessionRegistry, type SessionRegistry } from "../telemetry/sessionRegistry";

class FakeSocket {
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: ((ev?: { code: number }) => void) | null = null;
  onerror: (() => void) | null = null;
  binaryType = "blob";
  readyState = 0;
  send() {}
  close() {}
  error(code: string) {
    this.onopen?.();
    this.onmessage?.({ data: JSON.stringify({ type: "error", code, value: code }) });
  }
}

class FakeMediaSource {
  static isTypeSupported() {
    return true;
  }
  addEventListener() {}
}

let sockets: FakeSocket[];
beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("MediaSource", FakeMediaSource as unknown as typeof MediaSource);
  sockets = [];
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function start(resilient = true) {
  const session = new PlayerSession({
    cameraId: "cam-1",
    quality: "sub",
    serverId: "srv-1",
    resilient,
    random: () => 0,
    backoff: new ServerBackoff({ random: () => 0 }),
    metrics: createPlayerMetrics(),
    createSocket: () => {
      const s = new FakeSocket();
      sockets.push(s);
      return s as unknown as WebSocket;
    },
  });
  session.connect();
  return session;
}

describe("PlayerSession stream errors", () => {
  it.each([
    ["unauthorized", "Sesión expirada"],
    ["forbidden", "Sin permiso"],
    ["codec_unsupported", "Códec no soportado"],
  ])("stops retrying on %s until the user retries", (code, label) => {
    const session = start();
    sockets[0]!.error(code);
    expect(session.state).toBe("ERROR");
    expect(session.getSnapshot().error).toMatchObject({ code, label, retryable: false });
    sockets[0]!.onclose?.();
    vi.advanceTimersByTime(120_000);
    expect(sockets).toHaveLength(1);

    session.retryNow();
    expect(sockets).toHaveLength(2);
    expect(session.state).toBe("CONNECTING");
    expect(session.getSnapshot().error).toBeNull();
    session.close();
  });

  it.each([
    ["camera_offline", "Cámara sin conexión"],
    ["upstream_unreachable", "Servidor no disponible"],
    ["server_error", "Error de stream"],
  ])("keeps reconnecting with backoff on %s and clears the error with media", (code, label) => {
    const session = start();
    sockets[0]!.error(code);
    sockets[0]!.onclose?.();
    expect(session.state).toBe("RECONNECTING");
    expect(session.getSnapshot()).toMatchObject({ message: label, error: { code, retryable: true } });
    vi.advanceTimersByTime(5_000);
    expect(sockets).toHaveLength(2);

    sockets[1]!.onopen?.();
    sockets[1]!.onmessage?.({ data: new ArrayBuffer(8) });
    expect(session.state).toBe("ACTIVE");
    expect(session.getSnapshot().error).toBeNull();
    session.close();
  });

  it("retries camera_no_signal on a long fixed delay instead of hammering the gateway", () => {
    const session = start();
    sockets[0]!.error("camera_no_signal");
    sockets[0]!.onclose?.();
    expect(session.state).toBe("RECONNECTING");
    expect(session.getSnapshot()).toMatchObject({ message: "Cámara sin señal (Frigate)", error: { code: "camera_no_signal", retryable: true } });
    vi.advanceTimersByTime(29_000);
    expect(sockets).toHaveLength(1);
    vi.advanceTimersByTime(2_000);
    expect(sockets).toHaveLength(2);
    session.close();
  });

  it("treats a 1008 close without an error frame as an expired session", () => {
    const session = start();
    sockets[0]!.onopen?.();
    sockets[0]!.onclose?.({ code: 1008 });
    expect(session.getSnapshot().error?.code).toBe("unauthorized");
    vi.advanceTimersByTime(120_000);
    expect(sockets).toHaveLength(1);
    session.close();
  });

  it("keeps the legacy behaviour when not resilient: retries whatever the error", () => {
    const session = start(false);
    sockets[0]!.error("unauthorized");
    sockets[0]!.onclose?.();
    expect(session.getSnapshot().error).toBeNull();
    vi.advanceTimersByTime(5_000);
    expect(sockets).toHaveLength(2);
    session.close();
  });
});

describe("PlayerSession suspension", () => {
  it("closes the transport while suspended and reconnects on resume", () => {
    const session = start();
    sockets[0]!.onopen?.();
    session.setSuspended("offscreen", true);
    expect(session.state).toBe("SUSPENDED");
    vi.advanceTimersByTime(60_000);
    expect(sockets).toHaveLength(1);
    session.setSuspended("offscreen", false);
    expect(sockets).toHaveLength(2);
    session.close();
  });
});

describe("PlayerSession stream stats", () => {
  function create(registry?: SessionRegistry) {
    const session = new PlayerSession({
      cameraId: "cam-9",
      quality: "main",
      resilient: true,
      random: () => 0,
      backoff: new ServerBackoff({ random: () => 0 }),
      metrics: createPlayerMetrics(),
      registry,
      createSocket: () => {
        const s = new FakeSocket();
        sockets.push(s);
        return s as unknown as WebSocket;
      },
    });
    session.connect();
    return session;
  }

  /** Replaces the video's playback quality reading (jsdom has no getVideoPlaybackQuality). */
  function fakeQuality(session: PlayerSession, quality: { totalVideoFrames: number; droppedVideoFrames: number }) {
    (session.video as unknown as { getVideoPlaybackQuality: () => unknown }).getVideoPlaybackQuality = () => quality;
  }

  it("counts binary bytes from ArrayBuffer and Blob messages but not control messages", () => {
    const session = create();
    sockets[0]!.onopen?.();
    sockets[0]!.onmessage?.({ data: JSON.stringify({ type: "info", value: "ignored" }) });
    expect(session.streamStats().bytesReceived).toBe(0);
    sockets[0]!.onmessage?.({ data: new ArrayBuffer(1000) });
    sockets[0]!.onmessage?.({ data: new ArrayBuffer(24) });
    sockets[0]!.onmessage?.({ data: new Blob([new Uint8Array(76)]) });
    expect(session.streamStats().bytesReceived).toBe(1100);
    session.close();
  });

  it("keeps bytes monotonic across a reconnect", () => {
    const session = create();
    sockets[0]!.onopen?.();
    sockets[0]!.onmessage?.({ data: new ArrayBuffer(500) });
    sockets[0]!.onclose?.();
    vi.advanceTimersByTime(5_000);
    sockets[1]!.onopen?.();
    sockets[1]!.onmessage?.({ data: new ArrayBuffer(300) });
    expect(session.streamStats()).toMatchObject({ bytesReceived: 800, reconnects: 1 });
    session.close();
  });

  it("reports identity, state and the last error code", () => {
    const session = create();
    sockets[0]!.error("camera_offline");
    expect(session.streamStats()).toMatchObject({ cameraId: "cam-9", quality: "main", state: "RECONNECTING", lastErrorCode: "camera_offline", reconnects: 0 });
    session.close();
  });

  it("reads decoded and dropped frames and the picture size from the video when available", () => {
    const session = create();
    fakeQuality(session, { totalVideoFrames: 300, droppedVideoFrames: 7 });
    Object.defineProperty(session.video, "videoWidth", { value: 1920, configurable: true });
    Object.defineProperty(session.video, "videoHeight", { value: 1080, configurable: true });
    expect(session.streamStats()).toMatchObject({ decodedFrames: 300, droppedFrames: 7, width: 1920, height: 1080 });
    session.close();
  });

  it("leaves frame counters and size undefined when the platform does not expose them", () => {
    const session = create();
    const stats = session.streamStats();
    expect(stats.decodedFrames).toBeUndefined();
    expect(stats.droppedFrames).toBeUndefined();
    expect(stats.width).toBeUndefined();
    expect(stats.height).toBeUndefined();
    expect(stats.lastErrorCode).toBeUndefined();
    session.close();
  });

  it("counts stalls only while the session is active", () => {
    const session = create();
    session.video.dispatchEvent(new Event("waiting"));
    expect(session.streamStats().stalls).toBe(0);
    sockets[0]!.onopen?.();
    sockets[0]!.onmessage?.({ data: new ArrayBuffer(8) });
    expect(session.state).toBe("ACTIVE");
    session.video.dispatchEvent(new Event("waiting"));
    session.video.dispatchEvent(new Event("waiting"));
    expect(session.streamStats().stalls).toBe(2);
    session.close();
  });

  it("stops listening for stalls after close", () => {
    const session = create();
    sockets[0]!.onopen?.();
    sockets[0]!.onmessage?.({ data: new ArrayBuffer(8) });
    session.video.dispatchEvent(new Event("waiting"));
    expect(session.streamStats().stalls).toBe(1);
    session.close();
    session.video.dispatchEvent(new Event("waiting"));
    expect(session.streamStats().stalls).toBe(1);
  });

  it("registers into the registry on construction and unregisters on close", () => {
    const registry = createSessionRegistry();
    const session = create(registry);
    expect(registry.list()).toEqual([session]);
    session.close();
    expect(registry.list()).toEqual([]);
  });
});
