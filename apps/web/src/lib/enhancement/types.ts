/**
 * Types and interfaces for the non-destructive ImageEnhancementEngine.
 */

export type EnhancementMode = "off" | "auto" | "manual";

/**
 * Normalized Region of Interest (0..1) relative to the source frame.
 */
export type RoiRect = {
  x: number;
  y: number;
  width: number;
  height: number;
};

/**
 * Visual adjustment parameters.
 */
export type EnhancementAdjustments = {
  /** Brightness adjustment: -100 to 100, default 0 */
  brightness: number;
  /** Contrast adjustment: -100 to 100, default 0 */
  contrast: number;
  /** Gamma power curve: 0.2 to 3.0, default 1.0 (1.0 = linear) */
  gamma: number;
  /** Saturation adjustment: -100 to 100, default 0 */
  saturation: number;
  /** Unsharp mask / edge sharpening: 0 to 100, default 0 */
  sharpness: number;
  /** Black level normalization floor: 0.0 to 0.5, default 0.0 */
  blackLevel: number;
  /** White level normalization ceiling: 0.5 to 1.0, default 1.0 */
  whiteLevel: number;
  /** Color temperature shift (Kelvin-like warmth/coolness): -50 to 50, default 0 */
  temperature: number;
};

/**
 * Analyzed scene statistics from a frame or ROI sample.
 */
export type ImageStats = {
  minLuma: number;
  maxLuma: number;
  avgLuma: number;
  p5Luma: number;
  p95Luma: number;
  avgR: number;
  avgG: number;
  avgB: number;
  isDark: boolean;
  isOverexposed: boolean;
  isLowContrast: boolean;
};

/**
 * Core contract for non-destructive visual enhancement engines.
 */
export interface ImageEnhancementEngine {
  readonly mode: EnhancementMode;
  setMode(mode: EnhancementMode): void;
  setManualAdjustments(adjustments: Partial<EnhancementAdjustments>): void;
  getAdjustments(): EnhancementAdjustments;
  getTargetAdjustments(): EnhancementAdjustments;
  analyze(source: CanvasImageSource, roi?: RoiRect): ImageStats | null;
  calculateAdjustments(stats: ImageStats, roi?: RoiRect): EnhancementAdjustments;
  update(source: CanvasImageSource, roi?: RoiRect, deltaTimeMs?: number): EnhancementAdjustments;
  reset(): void;
  destroy(): void;
}
