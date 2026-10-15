import {
  computeAutoAdjustments,
  DEFAULT_ADJUSTMENTS,
  FULL_FRAME_ROI,
  interpolateAdjustments,
  sampleFrameStats,
} from "./analysis";
import type {
  EnhancementAdjustments,
  EnhancementMode,
  ImageEnhancementEngine,
  ImageStats,
  RoiRect,
} from "./types";

const ROI_EPSILON = 0.005;

function sameRoi(a: RoiRect, b: RoiRect): boolean {
  return (
    Math.abs(a.x - b.x) < ROI_EPSILON &&
    Math.abs(a.y - b.y) < ROI_EPSILON &&
    Math.abs(a.width - b.width) < ROI_EPSILON &&
    Math.abs(a.height - b.height) < ROI_EPSILON
  );
}

export class ImageEnhancementEngineImpl implements ImageEnhancementEngine {
  private _mode: EnhancementMode = "auto";
  private currentAdjustments: EnhancementAdjustments = { ...DEFAULT_ADJUSTMENTS };
  private targetAdjustments: EnhancementAdjustments = { ...DEFAULT_ADJUSTMENTS };
  private manualAdjustments: EnhancementAdjustments = { ...DEFAULT_ADJUSTMENTS };

  private lastAnalysisTime = 0;
  private readonly analysisIntervalMs = 250; // Throttled analysis (4 Hz) to prevent UI/CPU overhead
  /** Minimum gap between two analyses forced by a changed visible region (zoom/pan). */
  private readonly regionChangeIntervalMs = 50;
  private lastAnalysedRoi: RoiRect | null = null;
  private lastStats: ImageStats | null = null;

  get mode(): EnhancementMode {
    return this._mode;
  }

  setMode(mode: EnhancementMode): void {
    if (this._mode === mode) return;
    this._mode = mode;
    if (mode === "off") {
      this.targetAdjustments = { ...DEFAULT_ADJUSTMENTS };
    } else if (mode === "manual") {
      this.targetAdjustments = { ...this.manualAdjustments };
    }
  }

  setManualAdjustments(adjustments: Partial<EnhancementAdjustments>): void {
    this.manualAdjustments = {
      ...this.manualAdjustments,
      ...adjustments,
    };
    if (this._mode === "manual") {
      this.targetAdjustments = { ...this.manualAdjustments };
      this.currentAdjustments = { ...this.manualAdjustments };
    }
  }

  getAdjustments(): EnhancementAdjustments {
    if (this._mode === "off") {
      return { ...DEFAULT_ADJUSTMENTS };
    }
    return { ...this.currentAdjustments };
  }

  getTargetAdjustments(): EnhancementAdjustments {
    return { ...this.targetAdjustments };
  }

  analyze(source: CanvasImageSource, roi: RoiRect = FULL_FRAME_ROI): ImageStats | null {
    const stats = sampleFrameStats(source, roi);
    if (stats) this.lastStats = stats;
    return stats;
  }

  getLastStats(): ImageStats | null {
    return this.lastStats;
  }

  calculateAdjustments(stats: ImageStats, roi: RoiRect = FULL_FRAME_ROI): EnhancementAdjustments {
    return computeAutoAdjustments(stats, roi);
  }

  /**
   * Called during animation/render tick to update adjustments smoothly.
   */
  update(
    source: CanvasImageSource,
    roi: RoiRect = FULL_FRAME_ROI,
    nowMs: number = typeof performance !== "undefined" ? performance.now() : Date.now(),
  ): EnhancementAdjustments {
    if (this._mode === "off") {
      this.currentAdjustments = interpolateAdjustments(this.currentAdjustments, DEFAULT_ADJUSTMENTS, 0.25);
      return this.currentAdjustments;
    }

    if (this._mode === "manual") {
      this.currentAdjustments = interpolateAdjustments(this.currentAdjustments, this.targetAdjustments, 0.3);
      return this.currentAdjustments;
    }

    // AUTO mode:
    // A different visible region (zoom/pan) is analysed right away, so zooming into a dark area
    // brightens it without waiting for the next tick; the cost stays one small sample.
    const sinceLast = nowMs - this.lastAnalysisTime;
    const regionChanged = this.lastAnalysedRoi !== null && !sameRoi(this.lastAnalysedRoi, roi);
    if (sinceLast >= this.analysisIntervalMs || (regionChanged && sinceLast >= this.regionChangeIntervalMs)) {
      this.lastAnalysisTime = nowMs;
      this.lastAnalysedRoi = { ...roi };
      const stats = this.analyze(source, roi);
      if (stats) {
        this.targetAdjustments = this.calculateAdjustments(stats, roi);
      }
    }

    // Smoothly interpolate towards the target adjustments
    this.currentAdjustments = interpolateAdjustments(this.currentAdjustments, this.targetAdjustments, 0.15);
    return this.currentAdjustments;
  }

  reset(): void {
    this._mode = "auto";
    this.manualAdjustments = { ...DEFAULT_ADJUSTMENTS };
    this.targetAdjustments = { ...DEFAULT_ADJUSTMENTS };
    this.currentAdjustments = { ...DEFAULT_ADJUSTMENTS };
    this.lastStats = null;
    this.lastAnalysisTime = 0;
    this.lastAnalysedRoi = null;
  }

  destroy(): void {
    this.reset();
  }
}
