import { type DigitalZoom } from "@/lib/live/digitalZoom";
import { DEFAULT_ADJUSTMENTS, type EnhancementAdjustments, type ImageStats, type RoiRect } from "./types";
export { DEFAULT_ADJUSTMENTS, type EnhancementAdjustments, type ImageStats, type RoiRect };

export const FULL_FRAME_ROI: Readonly<RoiRect> = Object.freeze({
  x: 0,
  y: 0,
  width: 1,
  height: 1,
});

/**
 * Extract the visible normalized Region of Interest (0..1) from digital zoom.
 */
export function extractRoiRect(zoom: DigitalZoom, containerWidth: number, containerHeight: number): RoiRect {
  if (zoom.scale <= 1 || containerWidth <= 0 || containerHeight <= 0) {
    return { ...FULL_FRAME_ROI };
  }

  // The video element is translated by (zoom.x, zoom.y) and scaled by zoom.scale.
  // The visible viewport occupies [0, containerWidth] x [0, containerHeight] in parent space.
  const normX = -zoom.x / (zoom.scale * containerWidth);
  const normY = -zoom.y / (zoom.scale * containerHeight);
  const normW = 1 / zoom.scale;
  const normH = 1 / zoom.scale;

  const clampedX = Math.max(0, Math.min(1, normX));
  const clampedY = Math.max(0, Math.min(1, normY));
  const clampedW = Math.max(0.01, Math.min(1 - clampedX, normW));
  const clampedH = Math.max(0.01, Math.min(1 - clampedY, normH));

  return {
    x: clampedX,
    y: clampedY,
    width: clampedW,
    height: clampedH,
  };
}

let sharedAnalysisCanvas: HTMLCanvasElement | null = null;
let sharedAnalysisCtx: CanvasRenderingContext2D | null = null;

function getAnalysisContext(size: number): CanvasRenderingContext2D | null {
  if (typeof document === "undefined") return null;
  if (!sharedAnalysisCanvas) {
    sharedAnalysisCanvas = document.createElement("canvas");
    sharedAnalysisCanvas.width = size;
    sharedAnalysisCanvas.height = size;
    sharedAnalysisCtx = sharedAnalysisCanvas.getContext("2d", { willReadFrequently: true });
  }
  if (sharedAnalysisCanvas.width !== size) {
    sharedAnalysisCanvas.width = size;
    sharedAnalysisCanvas.height = size;
  }
  return sharedAnalysisCtx;
}

/**
 * Sample pixel statistics of the given ROI from a video or canvas source.
 * Uses a small 64x64 offscreen downsample to keep CPU load in microseconds.
 */
export function sampleFrameStats(
  source: CanvasImageSource,
  roi: RoiRect = FULL_FRAME_ROI,
  sampleSize = 64,
): ImageStats | null {
  const ctx = getAnalysisContext(sampleSize);
  if (!ctx) return null;

  try {
    const sw =
      "videoWidth" in source
        ? (source.videoWidth as number)
        : "displayWidth" in source
          ? (source.displayWidth as number)
          : "width" in source
            ? (source.width as number)
            : 0;
    const sh =
      "videoHeight" in source
        ? (source.videoHeight as number)
        : "displayHeight" in source
          ? (source.displayHeight as number)
          : "height" in source
            ? (source.height as number)
            : 0;
    if (!sw || !sh) return null;

    const sx = roi.x * sw;
    const sy = roi.y * sh;
    const sWidth = Math.max(1, roi.width * sw);
    const sHeight = Math.max(1, roi.height * sh);

    ctx.drawImage(source, sx, sy, sWidth, sHeight, 0, 0, sampleSize, sampleSize);
    const imgData = ctx.getImageData(0, 0, sampleSize, sampleSize);
    const data = imgData.data;
    const totalPixels = sampleSize * sampleSize;

    const histogram = new Uint32Array(256);
    let sumLuma = 0;
    let sumR = 0;
    let sumG = 0;
    let sumB = 0;
    let minLuma = 255;
    let maxLuma = 0;

    for (let i = 0; i < data.length; i += 4) {
      const r = data[i] ?? 0;
      const g = data[i + 1] ?? 0;
      const b = data[i + 2] ?? 0;

      // ITU-R BT.709 luminance
      const luma = Math.round(0.2126 * r + 0.7152 * g + 0.0722 * b);
      histogram[luma] = (histogram[luma] ?? 0) + 1;
      sumLuma += luma;
      sumR += r;
      sumG += g;
      sumB += b;

      if (luma < minLuma) minLuma = luma;
      if (luma > maxLuma) maxLuma = luma;
    }

    const avgLuma = Math.round(sumLuma / totalPixels);
    const avgR = Math.round(sumR / totalPixels);
    const avgG = Math.round(sumG / totalPixels);
    const avgB = Math.round(sumB / totalPixels);

    // Percentiles to ignore outlier dead or blown pixels
    let count = 0;
    let p5Luma = minLuma;
    let p95Luma = maxLuma;
    const targetP5 = totalPixels * 0.05;
    const targetP95 = totalPixels * 0.95;

    for (let val = 0; val < 256; val++) {
      count += histogram[val] ?? 0;
      if (count >= targetP5 && p5Luma === minLuma) {
        p5Luma = val;
      }
      if (count >= targetP95) {
        p95Luma = val;
        break;
      }
    }

    const isDark = avgLuma < 75;
    const isOverexposed = avgLuma > 185;
    const isLowContrast = p95Luma - p5Luma < 110;

    return {
      minLuma,
      maxLuma,
      avgLuma,
      p5Luma,
      p95Luma,
      avgR,
      avgG,
      avgB,
      isDark,
      isOverexposed,
      isLowContrast,
    };
  } catch {
    return null;
  }
}

/**
 * Compute auto enhancement adjustments based on scene statistics and ROI.
 */
export function computeAutoAdjustments(
  stats: ImageStats,
  roi: RoiRect = FULL_FRAME_ROI,
): EnhancementAdjustments {
  let brightness = 0;
  let contrast = 0;
  let gamma = 1.0;
  let blackLevel = 0.0;
  let whiteLevel = 1.0;
  let saturation = 0;
  let sharpness = 0;
  let temperature = 0;

  // 1. Black & White Level normalization (stretch dynamic range without clipping)
  if (stats.p5Luma > 18) {
    // Washed-out or foggy: stretch blacks
    blackLevel = Math.min(0.25, Number(((stats.p5Luma - 10) / 255 * 0.7).toFixed(3)));
  }
  if (stats.p95Luma < 225) {
    // Low highlight ceiling: expand highlights
    whiteLevel = Math.max(0.75, Number(((stats.p95Luma + 15) / 255).toFixed(3)));
  }

  // 2. Contrast correction
  const dynamicRange = stats.p95Luma - stats.p5Luma;
  if (dynamicRange < 160 || stats.p5Luma > 30) {
    // Scene lacks dynamic range or has elevated blacks: add contrast boost (10..35)
    const baseContrast = Math.max(8, (160 - dynamicRange) * 0.35);
    const elevatedBlackBonus = stats.p5Luma > 30 ? Math.min(15, (stats.p5Luma - 30) * 0.5) : 0;
    contrast = Math.round(Math.min(35, baseContrast + elevatedBlackBonus));
  }

  // 3. Gamma & Brightness curve (lift shadows safely)
  if (stats.isDark) {
    // Dark scene: lift gamma to open shadows without blowing highlights
    const darkRatio = Math.max(0, (75 - stats.avgLuma) / 75);
    gamma = Number((1.0 + darkRatio * 0.38).toFixed(2));
    brightness = Math.round(darkRatio * 20);
    // Dark scenes with gamma lift benefit from modest contrast boost
    contrast = Math.max(contrast, Math.round(darkRatio * 15));
  } else if (stats.isOverexposed) {
    // Overexposed scene: compress highlights slightly
    const overRatio = Math.max(0, (stats.avgLuma - 185) / 70);
    gamma = Number((1.0 - overRatio * 0.18).toFixed(2));
    brightness = Math.round(-overRatio * 12);
  }

  // 4. Color temperature / white balance correction
  // Gray-world assumption: if R channel substantially dominates B, scene is warm/amber; if B dominates, scene is cool.
  const colorDiff = stats.avgR - stats.avgB;
  if (Math.abs(colorDiff) > 15) {
    // Subtle balance shift toward neutral (-15..15)
    temperature = Math.round(Math.max(-15, Math.min(15, -colorDiff * 0.25)));
  }

  // 5. Saturation
  if (stats.isDark) {
    // Low-light video often suffers color desaturation
    saturation = 10;
  }

  // 6. Sharpening when digitally zoomed
  // When zoomed into an ROI (< 65% of full frame), apply light unsharp mask sharpening
  const roiArea = roi.width * roi.height;
  if (roiArea < 0.65) {
    const zoomStrength = Math.min(1, (0.65 - roiArea) / 0.6);
    // Keep between 10 and 35 to prevent noise halos
    sharpness = Math.round(10 + zoomStrength * 25);
  }

  return {
    brightness,
    contrast,
    gamma,
    saturation,
    sharpness,
    blackLevel,
    whiteLevel,
    temperature,
  };
}

/**
 * Linearly interpolate between two adjustment states for smooth, flicker-free transitions.
 */
export function interpolateAdjustments(
  current: EnhancementAdjustments,
  target: EnhancementAdjustments,
  alpha = 0.2,
): EnhancementAdjustments {
  const lerp = (curr: number, tgt: number, threshold = 0.01) => {
    if (Math.abs(tgt - curr) < threshold) return tgt;
    return curr + (tgt - curr) * alpha;
  };

  return {
    brightness: lerp(current.brightness, target.brightness),
    contrast: lerp(current.contrast, target.contrast),
    gamma: lerp(current.gamma, target.gamma, 0.005),
    saturation: lerp(current.saturation, target.saturation),
    sharpness: lerp(current.sharpness, target.sharpness),
    blackLevel: lerp(current.blackLevel, target.blackLevel, 0.002),
    whiteLevel: lerp(current.whiteLevel, target.whiteLevel, 0.002),
    temperature: lerp(current.temperature, target.temperature),
  };
}
