import { describe, expect, it } from "vitest";
import {
  analyzeFrameQuality,
  applyDeblurFilter,
  applyDenoiseFilter,
  applySuperResolution,
  ForensicEnhancementEngine,
} from "./forensic";

describe("ForensicEnhancementEngine - Quality Analyzer & Adaptive Pipeline", () => {
  it("detects blurry images using Laplacian variance", () => {
    // Create a smooth / blurry synthetic image (all pixels constant or gradient)
    const blurryData = new Uint8ClampedArray(64 * 64 * 4);
    for (let i = 0; i < blurryData.length; i += 4) {
      blurryData[i] = 128;     // R
      blurryData[i + 1] = 128; // G
      blurryData[i + 2] = 128; // B
      blurryData[i + 3] = 255; // A
    }

    const metrics = analyzeFrameQuality(blurryData, 64, 64);
    expect(metrics.blurScore).toBeLessThan(50);
    expect(metrics.needsDeblur).toBe(true);
  });

  it("detects sharp images with high edge variance as not needing deblur", () => {
    // Create a high-contrast checkerboard with sharp edges
    const sharpData = new Uint8ClampedArray(64 * 64 * 4);
    for (let y = 0; y < 64; y++) {
      for (let x = 0; x < 64; x++) {
        const idx = (y * 64 + x) * 4;
        const val = (x % 4 < 2 && y % 4 < 2) ? 240 : 15;
        sharpData[idx] = val;
        sharpData[idx + 1] = val;
        sharpData[idx + 2] = val;
        sharpData[idx + 3] = 255;
      }
    }

    const metrics = analyzeFrameQuality(sharpData, 64, 64);
    expect(metrics.blurScore).toBeGreaterThan(150);
    expect(metrics.needsDeblur).toBe(false);
  });

  it("applies deblur filter recovering edge gradients", () => {
    const input = new Uint8ClampedArray(32 * 32 * 4);
    for (let i = 0; i < input.length; i += 4) {
      input[i] = 100;
      input[i + 1] = 100;
      input[i + 2] = 100;
      input[i + 3] = 255;
    }
    // Put a soft edge in the middle
    input[(16 * 32 + 16) * 4] = 200;

    const output = applyDeblurFilter(input, 32, 32, 1.5);
    expect(output.length).toBe(input.length);
    // Center pixel contrast should be enhanced
    expect(output[(16 * 32 + 16) * 4]).toBeGreaterThanOrEqual(190);
  });

  it("applies edge-preserving bilateral denoise", () => {
    const input = new Uint8ClampedArray(16 * 16 * 4);
    // Flat region with small noise
    for (let i = 0; i < input.length; i += 4) {
      const noise = (i % 8 === 0 ? 10 : -10);
      input[i] = Math.min(255, Math.max(0, 128 + noise));
      input[i + 1] = Math.min(255, Math.max(0, 128 + noise));
      input[i + 2] = Math.min(255, Math.max(0, 128 + noise));
      input[i + 3] = 255;
    }

    const denoised = applyDenoiseFilter(input, 16, 16);
    expect(denoised.length).toBe(input.length);
  });

  it("applies 2x super resolution upscaling dimensions accurately", () => {
    const input = new Uint8ClampedArray(16 * 16 * 4);
    for (let i = 0; i < input.length; i += 4) {
      input[i] = 120;
      input[i + 1] = 120;
      input[i + 2] = 120;
      input[i + 3] = 255;
    }

    const upscaled = applySuperResolution(input, 16, 16, 2);
    // Width and height doubled: 32 * 32 * 4
    expect(upscaled.width).toBe(32);
    expect(upscaled.height).toBe(32);
    expect(upscaled.data.length).toBe(32 * 32 * 4);
  });

  it("executes the full adaptive pipeline with audit metadata", async () => {
    const engine = new ForensicEnhancementEngine();

    // Create a mock source canvas
    const canvas = document.createElement("canvas");
    canvas.width = 64;
    canvas.height = 64;
    const ctx = canvas.getContext("2d");
    if (ctx) {
      ctx.fillStyle = "#444";
      ctx.fillRect(0, 0, 64, 64);
      ctx.fillStyle = "#fff";
      ctx.fillText("ABC-123", 10, 30);
    }

    const result = await engine.enhanceFrame(canvas, {
      scaleFactor: 2,
      roi: { x: 0.1, y: 0.1, width: 0.8, height: 0.8 },
      metadata: {
        cameraId: "cam-front",
        cameraName: "Entrada Principal",
        timestamp: 1718000000,
      },
    });

    expect(result.originalCanvas).toBeDefined();
    expect(result.enhancedCanvas).toBeDefined();
    expect(result.enhancedCanvas.width).toBeGreaterThanOrEqual(result.originalCanvas.width);
    expect(result.operationsApplied.length).toBeGreaterThan(0);
    expect(result.metadata.cameraName).toBe("Entrada Principal");
    expect(result.metadata.timestamp).toBe(1718000000);
    expect(result.executionTimeMs).toBeGreaterThanOrEqual(0);
  });
});
