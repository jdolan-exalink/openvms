import { afterEach, describe, expect, it, vi } from "vitest";
import { detectClientCapabilities, gpuShortName } from "./clientCapabilities";

function stubCanvas(contexts: Record<string, unknown>) {
  vi.spyOn(document, "createElement").mockImplementation(((tag: string) => {
    if (tag !== "canvas") return Document.prototype.createElement.call(document, tag);
    return { getContext: (kind: string) => contexts[kind] ?? null } as unknown as HTMLCanvasElement;
  }) as typeof document.createElement);
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("detectClientCapabilities", () => {
  it("reads GPU info through WEBGL_debug_renderer_info when exposed", () => {
    const ext = { UNMASKED_VENDOR_WEBGL: 1, UNMASKED_RENDERER_WEBGL: 2 };
    const gl = {
      getExtension: (n: string) => (n === "WEBGL_debug_renderer_info" ? ext : null),
      getParameter: (p: number) => (p === 1 ? "NVIDIA Corporation" : "ANGLE (NVIDIA, NVIDIA GeForce RTX 3060 Direct3D11 vs_5_0 ps_5_0, D3D11)"),
    };
    stubCanvas({ webgl2: gl, webgl: gl });
    vi.stubGlobal("navigator", { hardwareConcurrency: 16, deviceMemory: 8, platform: "Win32", userAgent: "UA" });
    const caps = detectClientCapabilities();
    expect(caps.cpu.logicalProcessors).toBe(16);
    expect(caps.memory.reportedGb).toBe(8);
    expect(caps.gpu.webgl2).toBe(true);
    expect(caps.gpu.webgl).toBe(true);
    expect(caps.gpu.vendor).toBe("NVIDIA Corporation");
    expect(caps.gpu.renderer).toContain("RTX 3060");
    expect(caps.codecs).toEqual([]);
  });

  it("returns null GPU strings when the debug extension is missing and never invents values", () => {
    stubCanvas({ webgl: { getExtension: () => null, getParameter: () => "x" } });
    vi.stubGlobal("navigator", { userAgent: "UA" });
    const caps = detectClientCapabilities();
    expect(caps.gpu).toMatchObject({ webgl: true, webgl2: false, vendor: null, renderer: null });
    expect(caps.cpu.logicalProcessors).toBeNull();
    expect(caps.memory.reportedGb).toBeNull();
  });

  it("reports no WebGL when no context is available", () => {
    stubCanvas({});
    const caps = detectClientCapabilities();
    expect(caps.gpu).toMatchObject({ webgl: false, webgl2: false, renderer: null });
  });
});

describe("gpuShortName", () => {
  it("strips ANGLE wrapper and driver noise", () => {
    expect(gpuShortName("ANGLE (NVIDIA, NVIDIA GeForce RTX 3060 Direct3D11 vs_5_0 ps_5_0, D3D11)")).toBe("NVIDIA GeForce RTX 3060");
    expect(gpuShortName("Intel(R) UHD Graphics 630")).toBe("Intel(R) UHD Graphics 630");
    expect(gpuShortName(null)).toBeNull();
  });
});
