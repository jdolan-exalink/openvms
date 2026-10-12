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

export class ImageEnhancementEngineImpl implements ImageEnhancementEngine {
  private _mode: EnhancementMode = "auto";
  private currentAdjustments: EnhancementAdjustments = { ...DEFAULT_ADJUSTMENTS };
  private targetAdjustments: EnhancementAdjustments = { ...DEFAULT_ADJUSTMENTS };
  private manualAdjustments: EnhancementAdjustments = { ...DEFAULT_ADJUSTMENTS };

  private lastAnalysisTime = 0;
  private readonly analysisIntervalMs = 250; // Throttled analysis (4 Hz) to prevent UI/CPU overhead
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
    if (nowMs - this.lastAnalysisTime >= this.analysisIntervalMs) {
      this.lastAnalysisTime = nowMs;
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
  }

  destroy(): void {
    this.reset();
  }
}
