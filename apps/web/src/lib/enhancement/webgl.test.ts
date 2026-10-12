import { describe, expect, it } from "vitest";
import { DEFAULT_ADJUSTMENTS, type EnhancementAdjustments } from "./types";
import { generateCssFilterString, WebGLVisualEnhancer } from "./webgl";

describe("WebGLVisualEnhancer & Fallback Pipeline", () => {
  it("generates correct CSS filter string for fallback rendering", () => {
    expect(generateCssFilterString(DEFAULT_ADJUSTMENTS)).toBe("none");

    const custom: EnhancementAdjustments = {
      ...DEFAULT_ADJUSTMENTS,
      brightness: 20, // +20% -> 120%
      contrast: 15,   // +15% -> 115%
      saturation: -10, // -10% -> 90%
    };
    const filter = generateCssFilterString(custom);
    expect(filter).toContain("brightness(120%)");
    expect(filter).toContain("contrast(115%)");
    expect(filter).toContain("saturate(90%)");
  });

  it("normalizes shader uniforms correctly from human-readable adjustments", () => {
    const enhancer = new WebGLVisualEnhancer();
    const uniforms = enhancer.normalizeUniforms({
      brightness: 50,
      contrast: 30,
      gamma: 1.5,
      saturation: -20,
      sharpness: 40,
      blackLevel: 0.1,
      whiteLevel: 0.9,
      temperature: 20,
    });

    expect(uniforms.brightness).toBeCloseTo(0.5, 2);
    expect(uniforms.contrast).toBeCloseTo(0.3, 2);
    expect(uniforms.gamma).toBeCloseTo(1.5, 2);
    expect(uniforms.saturation).toBeCloseTo(-0.2, 2);
    expect(uniforms.sharpness).toBeCloseTo(0.4, 2);
    expect(uniforms.blackLevel).toBeCloseTo(0.1, 2);
    expect(uniforms.whiteLevel).toBeCloseTo(0.9, 2);
    expect(uniforms.temperature).toBeCloseTo(0.4, 2);
  });

  it("handles WebGL unsupported or context-lost environments gracefully without throwing", () => {
    const canvas = document.createElement("canvas");
    canvas.width = 320;
    canvas.height = 240;

    const enhancer = new WebGLVisualEnhancer();
    // In node/vitest, canvas.getContext("webgl") returns null by default
    const initialized = enhancer.init(canvas);
    expect(typeof initialized).toBe("boolean");

    // Rendering in fallback or unsupported state must not throw
    expect(() => {
      enhancer.render(canvas, DEFAULT_ADJUSTMENTS);
    }).not.toThrow();

    enhancer.destroy();
  });
});
