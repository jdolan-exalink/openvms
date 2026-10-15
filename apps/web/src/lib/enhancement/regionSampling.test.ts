import { beforeAll, describe, expect, it, vi } from "vitest";
import { FULL_FRAME_ROI, extractRoiRect, sampleFrameStats } from "./analysis";
import { ImageEnhancementEngineImpl } from "./engine";

const SAMPLE = 64;
/** The left half of the stub frame is bright, its right half is dark. */
const drawCalls: number[][] = [];
let lastSx = 0;

beforeAll(() => {
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockImplementation((() => ({
    drawImage: (...args: number[]) => {
      drawCalls.push(args.slice(1));
      lastSx = args[1] ?? 0;
    },
    getImageData: (_x: number, _y: number, w: number, h: number) => {
      const dark = lastSx >= 320;
      const data = new Uint8ClampedArray(w * h * 4);
      for (let i = 0; i < data.length; i += 4) {
        const v = dark ? 20 : 170;
        data[i] = v;
        data[i + 1] = v;
        data[i + 2] = v;
        data[i + 3] = 255;
      }
      return { data, width: w, height: h };
    },
  })) as unknown as HTMLCanvasElement["getContext"]);
});

const frame = () => ({ videoWidth: 1280, videoHeight: 720 }) as unknown as HTMLVideoElement;

describe("region-aware sampling", () => {
  it("maps a zoom/pan state to the visible normalized region", () => {
    const roi = extractRoiRect({ scale: 4, x: -2 * 400, y: -1 * 300 }, 400, 300);
    expect(roi.x).toBeCloseTo(0.5);
    expect(roi.y).toBeCloseTo(0.25);
    expect(roi.width).toBeCloseTo(0.25);
    expect(roi.height).toBeCloseTo(0.25);
  });

  it("samples the visible region of the frame at the same bounded size", () => {
    drawCalls.length = 0;
    sampleFrameStats(frame(), { x: 0.5, y: 0.25, width: 0.25, height: 0.25 });
    expect(drawCalls.at(-1)).toEqual([640, 180, 320, 180, 0, 0, SAMPLE, SAMPLE]);
    sampleFrameStats(frame(), FULL_FRAME_ROI);
    expect(drawCalls.at(-1)).toEqual([0, 0, 1280, 720, 0, 0, SAMPLE, SAMPLE]);
  });

  it("brightens a zoomed dark area that the full frame does not need", () => {
    const engine = new ImageEnhancementEngineImpl();
    engine.update(frame(), FULL_FRAME_ROI, 1000);
    const full = engine.getTargetAdjustments();
    const darkRegion = new ImageEnhancementEngineImpl();
    darkRegion.update(frame(), { x: 0.5, y: 0.25, width: 0.25, height: 0.25 }, 1000);
    const zoomed = darkRegion.getTargetAdjustments();
    expect(zoomed.gamma).toBeGreaterThan(full.gamma);
    expect(zoomed.brightness).toBeGreaterThan(full.brightness);
  });

  it("re-samples right away when the visible region changes inside the throttle window", () => {
    const engine = new ImageEnhancementEngineImpl();
    engine.update(frame(), FULL_FRAME_ROI, 1000);
    const before = engine.getTargetAdjustments();
    drawCalls.length = 0;
    engine.update(frame(), { x: 0.5, y: 0.25, width: 0.25, height: 0.25 }, 1100);
    expect(drawCalls).toHaveLength(1);
    expect(engine.getTargetAdjustments().gamma).toBeGreaterThan(before.gamma);
  });

  it("does not re-sample more often than today while the region is unchanged", () => {
    const engine = new ImageEnhancementEngineImpl();
    const roi = { x: 0.5, y: 0.25, width: 0.25, height: 0.25 };
    engine.update(frame(), roi, 1000);
    drawCalls.length = 0;
    engine.update(frame(), { ...roi }, 1100);
    expect(drawCalls).toHaveLength(0);
    engine.update(frame(), roi, 1250);
    expect(drawCalls).toHaveLength(1);
  });
});
