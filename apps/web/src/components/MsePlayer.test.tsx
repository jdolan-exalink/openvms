import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MsePlayer } from "./MsePlayer";

afterEach(() => vi.unstubAllGlobals());

/** FakeSocket lets tests drive the WebSocket lifecycle MsePlayer depends on. */
class FakeSocket {
  static instances: FakeSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 0;
  binaryType = "blob";
  constructor(public url: string) {
    FakeSocket.instances.push(this);
  }
  send() {}
  close() {}
}

class FakeMediaSource {
  static isTypeSupported() {
    return true;
  }
  addEventListener() {}
}

function stubBrowserAPIs() {
  FakeSocket.instances = [];
  vi.stubGlobal("WebSocket", FakeSocket as unknown as typeof WebSocket);
  vi.stubGlobal("MediaSource", FakeMediaSource as unknown as typeof MediaSource);
}

function lastSocket(): FakeSocket {
  const socket = FakeSocket.instances.at(-1);
  if (!socket) throw new Error("no WebSocket was created");
  return socket;
}

describe("MsePlayer", () => {
  it("shows a permanent message instead of spinning on Conectando when the gateway rejects the handshake", () => {
    stubBrowserAPIs();
    render(<MsePlayer cameraId="cam-1" />);

    // The gateway (media.Gateway.live) refuses the WebSocket upgrade outright when the
    // camera has no go2rtc restream (media.errNoStream): the socket closes without ever
    // opening, and the browser exposes no HTTP status or body for that failure.
    const socket = lastSocket();
    act(() => socket.onclose?.());

    expect(screen.getByText("La cámara no tiene una transmisión de video disponible.")).toBeInTheDocument();
    expect(screen.queryByText("Conectando…")).not.toBeInTheDocument();
    // No automatic retry is scheduled for this permanent condition.
    expect(FakeSocket.instances).toHaveLength(1);
  });

  it("keeps reconnecting with the generic message when a stream drops after opening", () => {
    vi.useFakeTimers();
    stubBrowserAPIs();
    render(<MsePlayer cameraId="cam-1" />);

    const socket = lastSocket();
    act(() => socket.onopen?.());
    act(() => socket.onclose?.());

    expect(screen.getByText("Conectando…")).toBeInTheDocument();
    act(() => vi.advanceTimersByTime(2000));
    expect(FakeSocket.instances.length).toBeGreaterThan(1);
    vi.useRealTimers();
  });
});
