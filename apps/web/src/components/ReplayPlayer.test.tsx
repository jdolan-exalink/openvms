import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { createRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HlsPlayerHandle } from "./HlsPlayer";
import { ReplayPlayer } from "./ReplayPlayer";

class FakeSourceBuffer {
  updating = false;
  mode = "";
  buffered = { length: 0, start: () => 0, end: () => 0 };
  appended: unknown[] = [];
  private listeners = new Map<string, (() => void)[]>();
  addEventListener(type: string, fn: () => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), fn]);
  }
  appendBuffer(data: unknown) {
    this.appended.push(data);
    this.updating = true;
    queueMicrotask(() => {
      this.updating = false;
      this.listeners.get("updateend")?.forEach((f) => f());
    });
  }
  remove() {}
}

class FakeMediaSource {
  static supported = true;
  static isTypeSupported() {
    return FakeMediaSource.supported;
  }
  static last: FakeMediaSource | undefined;
  readyState = "open";
  mime = "";
  buffer = new FakeSourceBuffer();
  constructor() {
    FakeMediaSource.last = this;
  }
  addEventListener(type: string, fn: () => void) {
    if (type === "sourceopen") queueMicrotask(fn);
  }
  addSourceBuffer(mime: string) {
    this.mime = mime;
    return this.buffer;
  }
}

class FakeWebSocket {
  static all: FakeWebSocket[] = [];
  url: string;
  binaryType = "";
  sent: string[] = [];
  closed = false;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  constructor(url: string) {
    this.url = url;
    FakeWebSocket.all.push(this);
    queueMicrotask(() => this.onopen?.());
  }
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.closed = true;
  }
  receive(data: unknown) {
    this.onmessage?.({ data });
  }
}

const flush = () => act(async () => { await Promise.resolve(); await Promise.resolve(); });

beforeEach(() => {
  FakeWebSocket.all = [];
  FakeMediaSource.supported = true;
  vi.stubGlobal("MediaSource", FakeMediaSource);
  vi.stubGlobal("WebSocket", FakeWebSocket);
  URL.createObjectURL = vi.fn(() => "blob:replay");
  URL.revokeObjectURL = vi.fn();
  HTMLMediaElement.prototype.load = vi.fn();
  HTMLMediaElement.prototype.play = vi.fn().mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("ReplayPlayer", () => {
  it("asks the gateway for the chosen window and offers the codecs this browser plays", async () => {
    render(<ReplayPlayer cameraId="cam-1" start={1000} end={4600.2} startOffset={30.7} quality="sub" ariaLabel="replay" />);
    await flush();
    const ws = FakeWebSocket.all[0]!;
    expect(ws.url).toBe(`ws://${location.host}/media/v1/cameras/cam-1/playback?start=1030&end=4601&quality=sub`);
    expect(ws.binaryType).toBe("arraybuffer");
    expect(JSON.parse(ws.sent[0]!)).toEqual({ type: "mse", value: expect.stringContaining("avc1.640029") });
  });

  it("feeds the segments to the video and clears the loading chip once it plays", async () => {
    render(<ReplayPlayer cameraId="c" start={1000} end={2000} ariaLabel="replay" />);
    await flush();
    const ws = FakeWebSocket.all[0]!;
    ws.receive(JSON.stringify({ type: "mse", value: 'video/mp4; codecs="avc1.640029"' }));
    ws.receive(new ArrayBuffer(8));
    ws.receive(new ArrayBuffer(8));
    await flush();
    expect(FakeMediaSource.last!.mime).toContain("avc1.640029");
    expect(FakeMediaSource.last!.buffer.appended.length).toBe(2);
    expect(screen.getByRole("status")).toHaveTextContent("Cargando grabación…");
    fireEvent(screen.getByLabelText("replay"), new Event("playing"));
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("shows the gateway's reason and does not reconnect when it refuses", async () => {
    render(<ReplayPlayer cameraId="c" start={1000} end={2000} />);
    await flush();
    const ws = FakeWebSocket.all[0]!;
    ws.receive(JSON.stringify({ type: "error", code: "upstream_unreachable", message: "x", value: "x" }));
    await flush();
    expect(screen.getByRole("alert")).toHaveTextContent("Servidor no disponible");
    expect(ws.closed).toBe(true);
    ws.onclose?.();
    await flush();
    expect(FakeWebSocket.all).toHaveLength(1); // one session, never retried on its own
    expect(screen.getByRole("alert")).toHaveTextContent("Servidor no disponible");
  });

  it("says there is no recording when the socket closes before any video", async () => {
    render(<ReplayPlayer cameraId="c" start={1000} end={2000} />);
    await flush();
    FakeWebSocket.all[0]!.onclose?.();
    await flush();
    expect(screen.getByRole("alert")).toHaveTextContent("No hay grabación en ese tramo.");
  });

  it("marks the end of the stretch after video, without covering the picture", async () => {
    render(<ReplayPlayer cameraId="c" start={1000} end={2000} />);
    await flush();
    const ws = FakeWebSocket.all[0]!;
    ws.receive(JSON.stringify({ type: "mse", value: 'video/mp4; codecs="avc1.640029"' }));
    ws.receive(new ArrayBuffer(4));
    await flush();
    ws.onclose?.();
    await flush();
    expect(screen.getByRole("status")).toHaveTextContent("Fin de la grabación de este tramo");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("starts a new session for a new instant and closes the old one quietly", async () => {
    const { rerender } = render(<ReplayPlayer cameraId="c" start={1000} end={5000} startOffset={0} />);
    await flush();
    const first = FakeWebSocket.all[0]!;
    rerender(<ReplayPlayer cameraId="c" start={1000} end={5000} startOffset={600} />);
    await flush();
    expect(first.closed).toBe(true);
    expect(FakeWebSocket.all).toHaveLength(2);
    expect(FakeWebSocket.all[1]!.url).toContain("start=1600&");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(); // the old socket closing is not an error
  });

  it("closes its socket and releases the video when it unmounts", async () => {
    const { unmount } = render(<ReplayPlayer cameraId="c" start={1000} end={2000} />);
    await flush();
    unmount();
    expect(FakeWebSocket.all[0]!.closed).toBe(true);
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:replay");
  });

  it("reports the time of the recording, keeps the speed at normal and stays out of the multi-camera sync", async () => {
    const onTime = vi.fn();
    const handle = createRef<HlsPlayerHandle>();
    render(<ReplayPlayer ref={handle} cameraId="c" start={1000} end={2000} startOffset={100} onTime={onTime} ariaLabel="replay" />);
    await flush();
    const el = screen.getByLabelText("replay") as HTMLVideoElement;
    Object.defineProperty(el, "currentTime", { value: 12, configurable: true });
    fireEvent.timeUpdate(el);
    expect(onTime).toHaveBeenCalledWith(1112);
    el.playbackRate = 2;
    fireEvent(el, new Event("ratechange"));
    expect(el.playbackRate).toBe(1);
    expect(handle.current?.video).toBeNull();
  });

  it("says so when the browser cannot play Media Source Extensions", async () => {
    FakeMediaSource.supported = false;
    render(<ReplayPlayer cameraId="c" start={1000} end={2000} />);
    await flush();
    expect(screen.getByRole("alert")).toHaveTextContent("El navegador no soporta Media Source Extensions.");
    expect(FakeWebSocket.all).toHaveLength(0);
  });
});
