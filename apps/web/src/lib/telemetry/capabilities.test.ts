import { describe, expect, it, vi } from "vitest";
import {
  collectClientCapabilities,
  describeCpu,
  describeMemory,
  type CapabilitiesEnv,
  type WebGLContextLike,
} from "./capabilities";

const CHROME_UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36";
const FIREFOX_UA = "Mozilla/5.0 (X11; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0";
const SAFARI_UA =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15";
const EDGE_UA = `${CHROME_UA} Edg/126.0.2592.81`;

function fakeGl(opts: { debugInfo?: boolean } = {}) {
  const loseContext = vi.fn();
  const gl: WebGLContextLike = {
    VENDOR: 0x1f00,
    RENDERER: 0x1f01,
    MAX_TEXTURE_SIZE: 0x0d33,
    MAX_VIEWPORT_DIMS: 0x0d3a,
    getExtension: (name: string) => {
      if (name === "WEBGL_debug_renderer_info" && opts.debugInfo !== false) {
        return { UNMASKED_VENDOR_WEBGL: 0x9245, UNMASKED_RENDERER_WEBGL: 0x9246 };
      }
      if (name === "WEBGL_lose_context") return { loseContext };
      return null;
    },
    getParameter: (p: number) => {
      const params: Record<number, unknown> = {
        0x9245: "NVIDIA Corporation",
        0x9246: "NVIDIA GeForce RTX 3060",
        0x1f00: "WebKit",
        0x1f01: "WebKit WebGL",
        0x0d33: 16384,
        0x0d3a: new Int32Array([32767, 32767]),
      };
      return params[p];
    },
  };
  return { gl, loseContext };
}

function baseEnv(overrides: Partial<CapabilitiesEnv> = {}): CapabilitiesEnv {
  const { gl } = fakeGl();
  return {
    navigator: { userAgent: CHROME_UA, hardwareConcurrency: 16, deviceMemory: 8 },
    screen: { width: 1920, height: 1080 },
    viewport: { width: 1900, height: 960 },
    devicePixelRatio: 1,
    createWebGLContext: () => gl,
    mediaCapabilities: {
      decodingInfo: async () => ({ supported: true, smooth: true, powerEfficient: false }),
    },
    isTypeSupported: () => true,
    now: () => 1234,
    ...overrides,
  };
}

describe("collectClientCapabilities: system", () => {
  it("uses userAgentData high entropy values when available", async () => {
    const caps = await collectClientCapabilities(
      baseEnv({
        navigator: {
          userAgent: CHROME_UA,
          hardwareConcurrency: 8,
          userAgentData: {
            platform: "Windows",
            getHighEntropyValues: async () => ({
              platform: "Windows",
              platformVersion: "15.0.0",
              architecture: "x86",
              fullVersionList: [
                { brand: "Not/A)Brand", version: "8.0.0.0" },
                { brand: "Google Chrome", version: "126.0.6478.127" },
                { brand: "Chromium", version: "126.0.6478.127" },
              ],
            }),
          },
        },
      }),
    );
    expect(caps.runtime).toBe("browser");
    expect(caps.collectedAt).toBe(1234);
    expect(caps.system).toMatchObject({
      os: "Windows",
      osVersion: "15.0.0",
      architecture: "x86",
      browser: "Chrome",
      browserVersion: "126.0.6478.127",
      userAgent: CHROME_UA,
      screen: { width: 1920, height: 1080, devicePixelRatio: 1 },
      viewport: { width: 1900, height: 960 },
    });
  });

  it("falls back to userAgent parsing for Firefox", async () => {
    const caps = await collectClientCapabilities(baseEnv({ navigator: { userAgent: FIREFOX_UA } }));
    expect(caps.system).toMatchObject({ os: "Linux", browser: "Firefox", browserVersion: "127.0" });
    expect(caps.system.architecture).toBeUndefined();
  });

  it("recognises Edge before Chrome and Safari by its version token", async () => {
    const edge = await collectClientCapabilities(baseEnv({ navigator: { userAgent: EDGE_UA } }));
    expect(edge.system).toMatchObject({ os: "Windows", browser: "Edge", browserVersion: "126.0.2592.81" });
    const safari = await collectClientCapabilities(baseEnv({ navigator: { userAgent: SAFARI_UA } }));
    expect(safari.system).toMatchObject({ os: "macOS", browser: "Safari", browserVersion: "17.5" });
  });

  it("keeps the userAgent fallback when high entropy values reject", async () => {
    const caps = await collectClientCapabilities(
      baseEnv({
        navigator: {
          userAgent: CHROME_UA,
          userAgentData: {
            platform: "Windows",
            getHighEntropyValues: async () => {
              throw new Error("denied");
            },
          },
        },
      }),
    );
    expect(caps.system).toMatchObject({ os: "Windows", browser: "Chrome", browserVersion: "126.0.0.0" });
  });

  it("reports only whether there are multiple screens, never an invented exact count", async () => {
    const extended = await collectClientCapabilities(baseEnv({ screen: { width: 1, height: 1, isExtended: true } }));
    expect(extended.system.multipleScreens).toBe(true);
    expect(extended.system.screens).toBeUndefined();
    const plain = await collectClientCapabilities(baseEnv({ screen: { width: 1, height: 1, isExtended: false } }));
    expect(plain.system.multipleScreens).toBe(false);
    const unknown = await collectClientCapabilities(baseEnv({ screen: { width: 1, height: 1 } }));
    expect(unknown.system.multipleScreens).toBeUndefined();
  });
});

describe("collectClientCapabilities: cpu and memory", () => {
  it("reports logical processors and never invents a cpu model", async () => {
    const caps = await collectClientCapabilities(baseEnv());
    expect(caps.cpu).toEqual({ logicalProcessors: 16 });
    expect(describeCpu(caps)).toBe("16 logical processors");
  });

  it("reports deviceMemory as a bucketed lower bound", async () => {
    const caps = await collectClientCapabilities(baseEnv());
    expect(caps.memory).toEqual({ reportedGB: 8, bucketed: true, source: "deviceMemory" });
    expect(describeMemory(caps)).toBe(">= 8 GB");
  });

  it("marks memory unavailable when deviceMemory is missing", async () => {
    const caps = await collectClientCapabilities(baseEnv({ navigator: { userAgent: FIREFOX_UA } }));
    expect(caps.memory).toEqual({ bucketed: false, source: "unavailable" });
    expect(describeMemory(caps)).toBe("unavailable");
    expect(describeCpu(caps)).toBe("unavailable");
  });
});

describe("collectClientCapabilities: gpu", () => {
  it("reads unmasked vendor and renderer from webgl2 and loses the context", async () => {
    const { gl, loseContext } = fakeGl();
    const kinds: string[] = [];
    const caps = await collectClientCapabilities(
      baseEnv({
        createWebGLContext: (kind) => {
          kinds.push(kind);
          return gl;
        },
      }),
    );
    expect(kinds).toEqual(["webgl2", "webgl"]); // webgl2 detected first, webgl probed for the flag
    expect(caps.gpu).toMatchObject({
      webgl: true,
      webgl2: true,
      vendor: "NVIDIA Corporation",
      renderer: "NVIDIA GeForce RTX 3060",
      maxTextureSize: 16384,
      maxViewportDims: [32767, 32767],
    });
    expect(loseContext).toHaveBeenCalledTimes(2);
  });

  it("falls back to masked VENDOR/RENDERER without the debug extension", async () => {
    const { gl } = fakeGl({ debugInfo: false });
    const caps = await collectClientCapabilities(baseEnv({ createWebGLContext: () => gl }));
    expect(caps.gpu).toMatchObject({ vendor: "WebKit", renderer: "WebKit WebGL" });
  });

  it("treats missing webgl as absence, not an error", async () => {
    const caps = await collectClientCapabilities(baseEnv({ createWebGLContext: () => null }));
    expect(caps.gpu).toEqual({ webgl: false, webgl2: false });
  });

  it("treats a throwing context factory as absence", async () => {
    const caps = await collectClientCapabilities(
      baseEnv({
        createWebGLContext: () => {
          throw new Error("no canvas");
        },
      }),
    );
    expect(caps.gpu).toEqual({ webgl: false, webgl2: false });
  });
});

describe("collectClientCapabilities: codecs", () => {
  it("declares a codec level high enough for each profile's resolution", async () => {
    type Config = { video: { contentType: string; height: number } };
    const decodingInfo = vi.fn<(config: Config) => Promise<{ supported: boolean }>>(async () => ({ supported: true }));
    await collectClientCapabilities(baseEnv({ mediaCapabilities: { decodingInfo } }));
    const byHeight = (prefix: string) =>
      Object.fromEntries(
        decodingInfo.mock.calls
          .map(([config]) => config.video)
          .filter((video) => video.contentType.includes(prefix))
          .map((video) => [video.height, /codecs="([^"]+)"/.exec(video.contentType)?.[1]]),
      );
    expect(byHeight("avc1")).toEqual({ 360: "avc1.42E01E", 720: "avc1.640028", 1080: "avc1.640028", 2160: "avc1.640033" });
    expect(byHeight("hvc1")).toEqual({ 360: "hvc1.1.6.L120.90", 720: "hvc1.1.6.L120.90", 1080: "hvc1.1.6.L120.90", 2160: "hvc1.1.6.L150.90" });
    expect(byHeight("vp09")).toEqual({ 360: "vp09.00.21.08", 720: "vp09.00.31.08", 1080: "vp09.00.40.08", 2160: "vp09.00.50.08" });
    expect(byHeight("av01")).toEqual({ 360: "av01.0.01M.08", 720: "av01.0.05M.08", 1080: "av01.0.08M.08", 2160: "av01.0.12M.08" });
  });

  it("queries the full codec x profile matrix through mediaCapabilities", async () => {
    const decodingInfo = vi.fn(async () => ({ supported: true, smooth: false, powerEfficient: true }));
    const caps = await collectClientCapabilities(baseEnv({ mediaCapabilities: { decodingInfo } }));
    expect(caps.codecs).toHaveLength(30);
    expect(decodingInfo).toHaveBeenCalledTimes(30);
    expect(caps.codecs[0]).toMatchObject({
      codec: "h264",
      supported: true,
      smooth: false,
      powerEfficient: true,
      source: "mediaCapabilities",
    });
    const call = decodingInfo.mock.calls[0] as unknown as [
      { type: string; video: { contentType: string; width: number; framerate: number; bitrate: number } },
    ];
    expect(call[0].type).toBe("media-source");
    expect(call[0].video.contentType).toContain("avc1");
    expect(call[0].video.bitrate).toBeGreaterThan(0);
  });

  it("light mode queries only the videowall profile once per codec", async () => {
    const decodingInfo = vi.fn(async () => ({ supported: true, smooth: true, powerEfficient: true }));
    const caps = await collectClientCapabilities(baseEnv({ mediaCapabilities: { decodingInfo } }), {
      codecs: "light",
    });
    expect(decodingInfo).toHaveBeenCalledTimes(5);
    expect(caps.codecs.map((c) => c.codec)).toEqual(["h264", "hevc", "vp8", "vp9", "av1"]);
    for (const c of caps.codecs) {
      expect(c.profile).toMatchObject({ width: 640, height: 360, fps: 15 });
    }
  });

  it("marks a codec unsupported when decodingInfo rejects", async () => {
    const caps = await collectClientCapabilities(
      baseEnv({
        mediaCapabilities: {
          decodingInfo: async () => {
            throw new TypeError("bad config");
          },
        },
        isTypeSupported: undefined,
      }),
      { codecs: "light" },
    );
    expect(caps.codecs.every((c) => !c.supported)).toBe(true);
    expect(caps.codecs.every((c) => c.source === "mediaCapabilities")).toBe(true);
  });

  it("falls back to isTypeSupported without smooth or powerEfficient", async () => {
    const caps = await collectClientCapabilities(
      baseEnv({ mediaCapabilities: undefined, isTypeSupported: (mime) => mime.includes("avc1") }),
      { codecs: "light" },
    );
    const h264 = caps.codecs.find((c) => c.codec === "h264");
    const av1 = caps.codecs.find((c) => c.codec === "av1");
    expect(h264).toMatchObject({ supported: true, source: "isTypeSupported" });
    expect(h264?.smooth).toBeUndefined();
    expect(h264?.powerEfficient).toBeUndefined();
    expect(av1).toMatchObject({ supported: false, source: "isTypeSupported" });
  });

  it("reports unavailable when no codec API exists", async () => {
    const caps = await collectClientCapabilities(baseEnv({ mediaCapabilities: undefined, isTypeSupported: undefined }), {
      codecs: "light",
    });
    expect(caps.codecs.every((c) => !c.supported && c.source === "unavailable")).toBe(true);
  });
});

describe("collectClientCapabilities: per-query timeout", () => {
  const never = () => new Promise<never>(() => undefined);

  it("marks a codec unsupported when decodingInfo never answers", async () => {
    vi.useFakeTimers();
    try {
      const pending = collectClientCapabilities(
        baseEnv({ mediaCapabilities: { decodingInfo: never } }),
        { codecs: "light", queryTimeoutMs: 500 },
      );
      await vi.advanceTimersByTimeAsync(600);
      const caps = await pending;
      expect(caps.codecs).toHaveLength(5);
      for (const c of caps.codecs) {
        expect(c.supported).toBe(false);
        expect(c.source).toBe("mediaCapabilities");
      }
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps userAgent values when getHighEntropyValues never answers", async () => {
    vi.useFakeTimers();
    try {
      const pending = collectClientCapabilities(
        baseEnv({ navigator: { userAgent: CHROME_UA, userAgentData: { platform: "Windows", getHighEntropyValues: never } } }),
        { codecs: "light", queryTimeoutMs: 500 },
      );
      await vi.advanceTimersByTimeAsync(600);
      const caps = await pending;
      expect(caps.system.browser).toBe("Chrome");
      expect(caps.system.os).toBe("Windows");
      expect(caps.system.architecture).toBeUndefined();
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not delay answers that arrive in time and leaves no timers behind", async () => {
    vi.useFakeTimers();
    try {
      const caps = await collectClientCapabilities(baseEnv(), { codecs: "light", queryTimeoutMs: 500 });
      expect(caps.codecs.every((c) => c.supported)).toBe(true);
      expect(vi.getTimerCount()).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  });
});
