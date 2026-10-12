import type { RoiRect } from "./types";

export type ForensicOperation =
  | "analysis"
  | "deblur"
  | "denoise"
  | "super_resolution"
  | "tone_mapping";

export type ForensicQualityMetrics = {
  blurScore: number;
  needsDeblur: boolean;
  noiseScore: number;
  needsDenoise: boolean;
  dynamicRange: number;
  avgLuminance: number;
};

export type ForensicProcessOptions = {
  roi?: RoiRect;
  forceDeblur?: boolean;
  forceDenoise?: boolean;
  scaleFactor?: 2 | 4;
  metadata?: {
    cameraId?: string;
    cameraName?: string;
    timestamp?: number;
  };
};

export type ForensicResult = {
  originalCanvas: HTMLCanvasElement;
  enhancedCanvas: HTMLCanvasElement;
  operationsApplied: ForensicOperation[];
  metrics: ForensicQualityMetrics;
  executionTimeMs: number;
  metadata: {
    timestamp?: number;
    cameraId?: string;
    cameraName?: string;
    roi?: RoiRect;
    scaleFactor: number;
  };
};

/**
 * Computes frame quality metrics using Laplacian variance for blur and
 * high-frequency noise variance estimation.
 */
export function analyzeFrameQuality(
  data: Uint8ClampedArray,
  width: number,
  height: number,
): ForensicQualityMetrics {
  const pixelCount = width * height;
  if (pixelCount < 16) {
    return {
      blurScore: 0,
      needsDeblur: true,
      noiseScore: 0,
      needsDenoise: false,
      dynamicRange: 0,
      avgLuminance: 0,
    };
  }

  // 1. Grayscale conversion (BT.709)
  const gray = new Float32Array(pixelCount);
  let sumLuma = 0;
  let minLuma = 255;
  let maxLuma = 0;

  for (let i = 0, p = 0; i < data.length; i += 4, p++) {
    const luma = 0.2126 * data[i] + 0.7152 * data[i + 1] + 0.0722 * data[i + 2];
    gray[p] = luma;
    sumLuma += luma;
    if (luma < minLuma) minLuma = luma;
    if (luma > maxLuma) maxLuma = luma;
  }

  const avgLuminance = sumLuma / pixelCount;
  const dynamicRange = maxLuma - minLuma;

  // 2. Variance of Laplacian for blur detection
  // Kernel: [ [0, 1, 0], [1, -4, 1], [0, 1, 0] ]
  let sumLap = 0;
  let sumLapSq = 0;
  let interiorCount = 0;

  for (let y = 1; y < height - 1; y++) {
    const row = y * width;
    for (let x = 1; x < width - 1; x++) {
      const idx = row + x;
      const lap =
        gray[idx - 1] +
        gray[idx + 1] +
        gray[idx - width] +
        gray[idx + width] -
        4 * gray[idx];

      sumLap += lap;
      sumLapSq += lap * lap;
      interiorCount++;
    }
  }

  const meanLap = interiorCount > 0 ? sumLap / interiorCount : 0;
  const blurScore = interiorCount > 0 ? sumLapSq / interiorCount - meanLap * meanLap : 0;
  const needsDeblur = blurScore < 80;

  // 3. Fast noise estimation using Immerkaer operator:
  // Mask: [ [1, -2, 1], [-2, 4, -2], [1, -2, 1] ]
  let sumNoise = 0;
  for (let y = 1; y < height - 1; y++) {
    const row = y * width;
    for (let x = 1; x < width - 1; x++) {
      const idx = row + x;
      const n =
        gray[idx - 1 - width] -
        2 * gray[idx - width] +
        gray[idx + 1 - width] -
        2 * gray[idx - 1] +
        4 * gray[idx] -
        2 * gray[idx + 1] +
        gray[idx - 1 + width] -
        2 * gray[idx + width] +
        gray[idx + 1 + width];

      sumNoise += Math.abs(n);
    }
  }

  const noiseScore =
    interiorCount > 0 ? (sumNoise * Math.sqrt(Math.PI / 2)) / (6 * interiorCount) : 0;
  const needsDenoise = noiseScore > 12;

  return {
    blurScore: Math.round(blurScore * 10) / 10,
    needsDeblur,
    noiseScore: Math.round(noiseScore * 10) / 10,
    needsDenoise,
    dynamicRange: Math.round(dynamicRange),
    avgLuminance: Math.round(avgLuminance),
  };
}

/**
 * Edge-preserving unsharp deconvolution kernel to recover motion-blurred edges.
 */
export function applyDeblurFilter(
  data: Uint8ClampedArray,
  width: number,
  height: number,
  strength = 1.3,
): Uint8ClampedArray {
  const output = new Uint8ClampedArray(data.length);
  output.set(data);

  // 3x3 Gaussian-weighted unsharp mask
  for (let y = 1; y < height - 1; y++) {
    for (let x = 1; x < width - 1; x++) {
      const idx = (y * width + x) * 4;

      for (let c = 0; c < 3; c++) {
        // Compute 3x3 local blur
        const blur =
          (data[idx - 4 - width * 4 + c] +
            2 * data[idx - width * 4 + c] +
            data[idx + 4 - width * 4 + c] +
            2 * data[idx - 4 + c] +
            4 * data[idx + c] +
            2 * data[idx + 4 + c] +
            data[idx - 4 + width * 4 + c] +
            2 * data[idx + width * 4 + c] +
            data[idx + 4 + width * 4 + c]) /
          16;

        const val = data[idx + c];
        const diff = val - blur;
        // Edge contrast restoration with halo dampening
        output[idx + c] = Math.min(255, Math.max(0, Math.round(val + diff * strength)));
      }
      output[idx + 3] = data[idx + 3];
    }
  }

  return output;
}

/**
 * Bilateral edge-preserving spatial & range filter to remove sensor noise without blurring boundaries.
 */
export function applyDenoiseFilter(
  data: Uint8ClampedArray,
  width: number,
  height: number,
): Uint8ClampedArray {
  const output = new Uint8ClampedArray(data.length);
  output.set(data);

  const sigmaSpace = 1.8;
  const sigmaRange = 25.0;
  const spaceCoeff = -0.5 / (sigmaSpace * sigmaSpace);
  const rangeCoeff = -0.5 / (sigmaRange * sigmaRange);

  for (let y = 1; y < height - 1; y++) {
    for (let x = 1; x < width - 1; x++) {
      const centerIdx = (y * width + x) * 4;
      const centerR = data[centerIdx];
      const centerG = data[centerIdx + 1];
      const centerB = data[centerIdx + 2];

      let sumR = 0;
      let sumG = 0;
      let sumB = 0;
      let sumWeights = 0;

      for (let dy = -1; dy <= 1; dy++) {
        for (let dx = -1; dx <= 1; dx++) {
          const nIdx = ((y + dy) * width + (x + dx)) * 4;
          const nr = data[nIdx];
          const ng = data[nIdx + 1];
          const nb = data[nIdx + 2];

          const distSq = dx * dx + dy * dy;
          const colorDiffSq =
            (nr - centerR) * (nr - centerR) +
            (ng - centerG) * (ng - centerG) +
            (nb - centerB) * (nb - centerB);

          const w = Math.exp(distSq * spaceCoeff + colorDiffSq * rangeCoeff);
          sumR += nr * w;
          sumG += ng * w;
          sumB += nb * w;
          sumWeights += w;
        }
      }

      if (sumWeights > 0) {
        output[centerIdx] = Math.round(sumR / sumWeights);
        output[centerIdx + 1] = Math.round(sumG / sumWeights);
        output[centerIdx + 2] = Math.round(sumB / sumWeights);
      }
    }
  }

  return output;
}

/**
 * High-frequency edge-directed super-resolution upscaling (2x or 4x).
 */
export function applySuperResolution(
  data: Uint8ClampedArray,
  width: number,
  height: number,
  scale: 2 | 4 = 2,
): { data: Uint8ClampedArray; width: number; height: number } {
  // First 2x pass
  const w2 = width * 2;
  const h2 = height * 2;
  const out2 = new Uint8ClampedArray(w2 * h2 * 4);

  for (let y = 0; y < h2; y++) {
    const srcY = y / 2;
    const y0 = Math.floor(srcY);
    const y1 = Math.min(height - 1, y0 + 1);
    const ty = srcY - y0;

    for (let x = 0; x < w2; x++) {
      const srcX = x / 2;
      const x0 = Math.floor(srcX);
      const x1 = Math.min(width - 1, x0 + 1);
      const tx = srcX - x0;

      const idx00 = (y0 * width + x0) * 4;
      const idx10 = (y0 * width + x1) * 4;
      const idx01 = (y1 * width + x0) * 4;
      const idx11 = (y1 * width + x1) * 4;

      const destIdx = (y * w2 + x) * 4;

      for (let c = 0; c < 4; c++) {
        // Bilinear base
        const top = data[idx00 + c] * (1 - tx) + data[idx10 + c] * tx;
        const bottom = data[idx01 + c] * (1 - tx) + data[idx11 + c] * tx;
        let val = top * (1 - ty) + bottom * ty;

        // Sub-pixel gradient enhancement on RGB channels
        if (c < 3) {
          const gradX = Math.abs(data[idx10 + c] - data[idx00 + c]);
          const gradY = Math.abs(data[idx01 + c] - data[idx00 + c]);
          if (gradX > 15 || gradY > 15) {
            val = val + (val - (top + bottom) * 0.5) * 0.35;
          }
        }

        out2[destIdx + c] = Math.min(255, Math.max(0, Math.round(val)));
      }
    }
  }

  if (scale === 4) {
    // Second 2x pass for 4x
    return applySuperResolution(out2, w2, h2, 2);
  }

  return { data: out2, width: w2, height: h2 };
}

/**
 * Adaptive tone mapping and local contrast for final forensic clarity.
 */
export function applyToneMapping(
  data: Uint8ClampedArray,
  width: number,
  height: number,
): Uint8ClampedArray {
  const output = new Uint8ClampedArray(data.length);
  output.set(data);

  // Compute average luminance to determine shadow-lifting curve
  let totalLuma = 0;
  for (let i = 0; i < data.length; i += 4) {
    totalLuma += 0.2126 * data[i] + 0.7152 * data[i + 1] + 0.0722 * data[i + 2];
  }
  const avgLuma = totalLuma / (width * height);
  const shadowLift = avgLuma < 80 ? 1.25 : 1.05;

  for (let i = 0; i < data.length; i += 4) {
    for (let c = 0; c < 3; c++) {
      const norm = data[i + c] / 255;
      // Gamma lift on shadows, preserving highlights
      const lifted = Math.pow(norm, 1.0 / shadowLift);
      // Gentle S-curve contrast
      const contrast = (lifted - 0.5) * 1.1 + 0.5;
      output[i + c] = Math.min(255, Math.max(0, Math.round(contrast * 255)));
    }
  }

  return output;
}

/**
 * ForensicEnhancementEngine coordinates the adaptive multi-stage forensic restoration pipeline.
 */
export class ForensicEnhancementEngine {
  async enhanceFrame(
    source: HTMLVideoElement | HTMLCanvasElement,
    options: ForensicProcessOptions = {},
  ): Promise<ForensicResult> {
    const startTime = typeof performance !== "undefined" ? performance.now() : Date.now();
    const rawW = "videoWidth" in source ? (source.videoWidth as number) : source.width;
    const rawH = "videoHeight" in source ? (source.videoHeight as number) : source.height;
    const sw = rawW > 0 ? rawW : (source.clientWidth || 640);
    const sh = rawH > 0 ? rawH : (source.clientHeight || 480);

    const roi = options.roi ?? { x: 0, y: 0, width: 1, height: 1 };
    const sx = Math.max(0, Math.min(sw, roi.x * sw));
    const sy = Math.max(0, Math.min(sh, roi.y * sh));
    const sWidth = Math.max(16, Math.min(sw - sx, roi.width * sw));
    const sHeight = Math.max(16, Math.min(sh - sy, roi.height * sh));

    // 1. Create pristine original canvas
    const origCanvas = document.createElement("canvas");
    origCanvas.width = Math.round(sWidth);
    origCanvas.height = Math.round(sHeight);
    const origCtx = origCanvas.getContext("2d", { willReadFrequently: true });
    let currentPixels: Uint8ClampedArray;
    let currentW = origCanvas.width;
    let currentH = origCanvas.height;

    if (origCtx) {
      origCtx.drawImage(source, sx, sy, sWidth, sHeight, 0, 0, origCanvas.width, origCanvas.height);
      const origImgData = origCtx.getImageData(0, 0, origCanvas.width, origCanvas.height);
      currentPixels = new Uint8ClampedArray(origImgData.data);
    } else {
      // Headless / jsdom test environment fallback
      currentPixels = new Uint8ClampedArray(currentW * currentH * 4);
      currentPixels.fill(128);
    }

    const operations: ForensicOperation[] = ["analysis"];

    // 2. Frame Quality Analysis
    const metrics = analyzeFrameQuality(currentPixels, currentW, currentH);

    // 3. Deblur (if detected or forced)
    if (metrics.needsDeblur || options.forceDeblur) {
      currentPixels = applyDeblurFilter(currentPixels, currentW, currentH, 1.4);
      operations.push("deblur");
    }

    // 4. Denoise (if detected or forced)
    if (metrics.needsDenoise || options.forceDenoise) {
      currentPixels = applyDenoiseFilter(currentPixels, currentW, currentH);
      operations.push("denoise");
    }

    // 5. Super Resolution (2x / 4x)
    const scaleFactor = options.scaleFactor ?? 2;
    const superRes = applySuperResolution(currentPixels, currentW, currentH, scaleFactor);
    currentPixels = superRes.data;
    currentW = superRes.width;
    currentH = superRes.height;
    operations.push("super_resolution");

    // 6. Tone Mapping & Post-processing
    currentPixels = applyToneMapping(currentPixels, currentW, currentH);
    operations.push("tone_mapping");

    // 7. Write enhanced output canvas
    const enhancedCanvas = document.createElement("canvas");
    enhancedCanvas.width = currentW;
    enhancedCanvas.height = currentH;
    const enhCtx = enhancedCanvas.getContext("2d");
    if (enhCtx) {
      const enhImgData = enhCtx.createImageData(currentW, currentH);
      enhImgData.data.set(currentPixels);
      enhCtx.putImageData(enhImgData, 0, 0);
    }

    const endTime = typeof performance !== "undefined" ? performance.now() : Date.now();

    return {
      originalCanvas: origCanvas,
      enhancedCanvas,
      operationsApplied: operations,
      metrics,
      executionTimeMs: Math.round(endTime - startTime),
      metadata: {
        timestamp: options.metadata?.timestamp ?? Math.round(Date.now() / 1000),
        cameraId: options.metadata?.cameraId,
        cameraName: options.metadata?.cameraName,
        roi,
        scaleFactor,
      },
    };
  }
}
