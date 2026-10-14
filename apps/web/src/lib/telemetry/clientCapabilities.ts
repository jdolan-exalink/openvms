/**
 * Static facts about the client station, computed once. Only what the browser really exposes:
 * a missing value is null, never a guess.
 */
export type ClientCapabilities = {
  cpu: { logicalProcessors: number | null };
  memory: {
    /** `navigator.deviceMemory`: coarse, capped by the browser (usually at 8). Treat as "at least". */
    reportedGb: number | null;
  };
  gpu: { webgl: boolean; webgl2: boolean; vendor: string | null; renderer: string | null };
  platform: { platform: string | null; userAgent: string | null };
  /** Filled by the codec probe in a later stage. */
  codecs: string[];
};

type DebugInfoExt = { UNMASKED_VENDOR_WEBGL: number; UNMASKED_RENDERER_WEBGL: number };

function readGpu(): ClientCapabilities["gpu"] {
  const gpu: ClientCapabilities["gpu"] = { webgl: false, webgl2: false, vendor: null, renderer: null };
  let canvas: HTMLCanvasElement;
  try {
    canvas = document.createElement("canvas");
  } catch {
    return gpu;
  }
  const open = (kind: string): WebGLRenderingContext | null => {
    try {
      return canvas.getContext(kind) as WebGLRenderingContext | null;
    } catch {
      return null;
    }
  };
  const gl2 = open("webgl2");
  const gl = gl2 ?? open("webgl");
  gpu.webgl2 = gl2 !== null;
  gpu.webgl = gl !== null;
  if (gl) {
    try {
      const ext = gl.getExtension("WEBGL_debug_renderer_info") as DebugInfoExt | null;
      if (ext) {
        const vendor = gl.getParameter(ext.UNMASKED_VENDOR_WEBGL);
        const renderer = gl.getParameter(ext.UNMASKED_RENDERER_WEBGL);
        gpu.vendor = typeof vendor === "string" && vendor ? vendor : null;
        gpu.renderer = typeof renderer === "string" && renderer ? renderer : null;
      }
      gl.getExtension("WEBGL_lose_context")?.loseContext();
    } catch {
      // keep whatever was read; unavailable fields stay null
    }
  }
  return gpu;
}

export function detectClientCapabilities(): ClientCapabilities {
  const nav = (typeof navigator === "undefined" ? {} : navigator) as Partial<Navigator> & { deviceMemory?: number };
  return {
    cpu: { logicalProcessors: typeof nav.hardwareConcurrency === "number" ? nav.hardwareConcurrency : null },
    memory: { reportedGb: typeof nav.deviceMemory === "number" ? nav.deviceMemory : null },
    gpu: readGpu(),
    platform: { platform: nav.platform ?? null, userAgent: nav.userAgent ?? null },
    codecs: [],
  };
}

let cached: ClientCapabilities | undefined;

/** Computed lazily, once per page load. */
export function getClientCapabilities(): ClientCapabilities {
  cached ??= detectClientCapabilities();
  return cached;
}

/** Shortens a WebGL renderer string for display ("ANGLE (NVIDIA, X Direct3D11 ..., D3D11)" -> "X"). */
export function gpuShortName(renderer: string | null): string | null {
  if (!renderer) return null;
  let name = renderer.trim();
  const angle = /^ANGLE \((.*)\)$/.exec(name);
  if (angle) {
    const parts = angle[1]!.split(",").map((p) => p.trim());
    name = parts.length > 1 ? parts[1]! : parts[0]!;
  }
  name = name.replace(/\s+(Direct3D\d*|OpenGL|Metal|Vulkan)\b.*$/i, "").trim();
  return name || null;
}
