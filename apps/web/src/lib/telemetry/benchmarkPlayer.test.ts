import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createBrowserBenchmarkDeps, createMsePlayer } from "./benchmarkPlayer";
import { BENCHMARK_CLIP } from "./benchmark";

class FakeSourceBuffer {
  mode = "";
  updating = false;
  appended: ArrayBuffer[] = [];
  private listeners = new Map<string, Set<() => void>>();
  addEventListener(type: string, cb: () => void) {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set());
    this.listeners.get(type)!.add(cb);
  }
  removeEventListener(type: string, cb: () => void) {
    this.listeners.get(type)?.delete(cb);
  }
  appendBuffer(data: ArrayBuffer) {
    this.appended.push(data);
  }
  emit(type: string) {
    for (const cb of [...(this.listeners.get(type) ?? [])]) cb();
  }
}

class FakeMediaSource {
  static instances: FakeMediaSource[] = [];
  static addThrows = false;
  readyState = "closed";
  sb = new FakeSourceBuffer();
  mime = "";
  ended = false;
  private listeners = new Map<string, Set<() => void>>();
  constructor() {
    FakeMediaSource.instances.push(this);
  }
  addEventListener(type: string, cb: () => void) {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set());
    this.listeners.get(type)!.add(cb);
  }
  removeEventListener(type: string, cb: () => void) {
    this.listeners.get(type)?.delete(cb);
  }
  addSourceBuffer(mime: string) {
    if (FakeMediaSource.addThrows) throw new Error("unsupported");
    this.mime = mime;
    return this.sb;
  }
  endOfStream() {
    this.ended = true;
  }
  emit(type: string) {
    if (type === "sourceopen") this.readyState = "open";
    for (const cb of [...(this.listeners.get(type) ?? [])]) cb();
  }
}

const clip = new ArrayBuffer(16);
let container: HTMLElement;
let revoke: ReturnType<typeof vi.fn<(url: string) => void>>;

beforeEach(() => {
  FakeMediaSource.instances = [];
  FakeMediaSource.addThrows = false;
  vi.stubGlobal("MediaSource", FakeMediaSource);
  (window as unknown as { MediaSource: unknown }).MediaSource = FakeMediaSource;
  let n = 0;
  revoke = vi.fn<(url: string) => void>();
  URL.createObjectURL = vi.fn(() => `blob:fake-${++n}`);
  URL.revokeObjectURL = revoke;
  vi.spyOn(HTMLMediaElement.prototype, "play").mockImplementation(async () => undefined);
  vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
  vi.spyOn(HTMLMediaElement.prototype, "load").mockImplementation(() => undefined);
  container = document.createElement("div");
  document.body.append(container);
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  container.remove();
});

function openAndFinish(ms: FakeMediaSource) {
  ms.emit("sourceopen");
  ms.sb.emit("updateend");
}

describe("createMsePlayer", () => {
  it("mounts a muted tile-sized video, appends the whole clip once and settles on updateend", async () => {
    const player = createMsePlayer(container, clip);
    const video = container.querySelector("video")!;
    expect(video).not.toBeNull();
    expect(video.muted).toBe(true);
    expect(video.playsInline).toBe(true);
    expect(video.width).toBe(160);
    expect(video.height).toBe(90);
    const started = player.start();
    const ms = FakeMediaSource.instances[0]!;
    expect(video.src).toBe("blob:fake-1");
    openAndFinish(ms);
    await started;
    expect(ms.mime).toBe(BENCHMARK_CLIP.mimeType);
    expect(ms.sb.mode).toBe("segments");
    expect(ms.sb.appended).toHaveLength(1);
    expect(ms.sb.appended[0]!.byteLength).toBe(16);
    expect(ms.ended).toBe(true);
    expect(HTMLMediaElement.prototype.play).toHaveBeenCalled();
    expect(player.stats().errors).toBe(0);
  });

  it("reports decoded and dropped frames from getVideoPlaybackQuality", () => {
    const player = createMsePlayer(container, clip);
    const video = container.querySelector("video")!;
    expect(player.stats().decodedFrames).toBeUndefined();
    Object.defineProperty(video, "getVideoPlaybackQuality", { value: () => ({ totalVideoFrames: 30, droppedVideoFrames: 2 }) });
    expect(player.stats()).toMatchObject({ decodedFrames: 30, droppedFrames: 2, stalls: 0, errors: 0 });
  });

  it("counts waiting only once playback began, and element errors always", () => {
    const player = createMsePlayer(container, clip);
    const video = container.querySelector("video")!;
    video.dispatchEvent(new Event("waiting"));
    expect(player.stats().stalls).toBe(0);
    video.dispatchEvent(new Event("playing"));
    video.dispatchEvent(new Event("waiting"));
    expect(player.stats().stalls).toBe(1);
    video.dispatchEvent(new Event("error"));
    expect(player.stats().errors).toBe(1);
  });

  it("loops by seeking back to the start when the clip ends", () => {
    createMsePlayer(container, clip);
    const video = container.querySelector("video")!;
    video.currentTime = 0;
    Object.defineProperty(video, "currentTime", { value: 3.9, writable: true });
    video.dispatchEvent(new Event("ended"));
    expect(video.currentTime).toBe(0);
    expect(HTMLMediaElement.prototype.play).toHaveBeenCalled();
  });

  it("settles with an error when the browser has no MediaSource", async () => {
    vi.unstubAllGlobals();
    delete (window as unknown as { MediaSource?: unknown }).MediaSource;
    const player = createMsePlayer(container, clip);
    await player.start();
    expect(player.stats().errors).toBe(1);
  });

  it("settles with an error when the codec is rejected", async () => {
    FakeMediaSource.addThrows = true;
    const player = createMsePlayer(container, clip);
    const started = player.start();
    FakeMediaSource.instances[0]!.emit("sourceopen");
    await started;
    expect(player.stats().errors).toBe(1);
  });

  it("settles with an error when the source never opens", async () => {
    vi.useFakeTimers();
    const player = createMsePlayer(container, clip, { startTimeoutMs: 1000 });
    const started = player.start();
    await vi.advanceTimersByTimeAsync(1000);
    await started;
    expect(player.stats().errors).toBe(1);
  });

  it("disposes once: removes the element and the media and revokes the object URL", async () => {
    const player = createMsePlayer(container, clip);
    const video = container.querySelector("video")!;
    const started = player.start();
    openAndFinish(FakeMediaSource.instances[0]!);
    await started;
    player.dispose();
    player.dispose();
    expect(container.querySelector("video")).toBeNull();
    expect(video.getAttribute("src")).toBeNull();
    expect(revoke).toHaveBeenCalledTimes(1);
    expect(revoke).toHaveBeenCalledWith("blob:fake-1");
    // A late event after disposal changes nothing.
    video.dispatchEvent(new Event("error"));
    expect(player.stats().errors).toBe(0);
  });

  it("disposing before start leaves nothing mounted and never opens a source", async () => {
    const player = createMsePlayer(container, clip);
    player.dispose();
    await player.start();
    expect(container.querySelector("video")).toBeNull();
    expect(FakeMediaSource.instances).toHaveLength(0);
  });
});

describe("createBrowserBenchmarkDeps", () => {
  it("downloads the clip once as an ArrayBuffer and rejects on a bad status", async () => {
    const buffer = new ArrayBuffer(4);
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, arrayBuffer: async () => buffer })));
    const deps = createBrowserBenchmarkDeps(container);
    await expect(deps.loadClip()).resolves.toBe(buffer);
    expect(fetch).toHaveBeenCalledWith(BENCHMARK_CLIP.url, expect.anything());
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: false, status: 404 })));
    await expect(createBrowserBenchmarkDeps(container).loadClip()).rejects.toThrow(/404/);
  });

  it("sleeps for the requested time and wakes early when aborted", async () => {
    vi.useFakeTimers();
    const deps = createBrowserBenchmarkDeps(container);
    let done = false;
    void deps.sleep(500).then(() => (done = true));
    await vi.advanceTimersByTimeAsync(499);
    expect(done).toBe(false);
    await vi.advanceTimersByTimeAsync(1);
    expect(done).toBe(true);

    const controller = new AbortController();
    let woke = false;
    void deps.sleep(10_000, controller.signal).then(() => (woke = true));
    controller.abort();
    await vi.advanceTimersByTimeAsync(0);
    expect(woke).toBe(true);
  });

  it("resolves immediately for a signal that is already aborted", async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(createBrowserBenchmarkDeps(container).sleep(10_000, controller.signal)).resolves.toBeUndefined();
  });
});
