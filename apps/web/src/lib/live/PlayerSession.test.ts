import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PlayerSession } from "./PlayerSession";
import { createPlayerMetrics } from "./playerMetrics";
import { ServerBackoff } from "./serverBackoff";

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
