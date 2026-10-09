import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

// The room planner is a vanilla script served from public/; load it the way the browser does.
const source = readFileSync(resolve(__dirname, "../../../public/room-planner/js/canvas.js"), "utf8");

interface Renderer {
  init(canvas: HTMLCanvasElement): void;
  setZoom(zoom: number, pivotX?: number, pivotY?: number): void;
  pan(dx: number, dy: number): void;
  setTheme(name: string): void;
  getTheme(): string;
  getZoom(): number;
  getOffset(): { x: number; y: number };
  exportPNG(fitFirst?: boolean): string;
}

function loadRenderer(toDataURL: () => string): Renderer {
  const noop = () => ({ width: 0 });
  const ctx = new Proxy({}, { get: () => noop, set: () => true });
  const canvas = {
    getContext: () => ctx,
    toDataURL,
    style: {},
    parentElement: { clientWidth: 800, clientHeight: 600 },
  } as unknown as HTMLCanvasElement;
  const model = {
    walls: [{ id: "w1", x1: 0, y1: 0, x2: 1000, y2: 0 }, { id: "w2", x1: 1000, y1: 0, x2: 1000, y2: 500 }],
    labels: [],
    doors: [],
    windows: [],
    rooms: [],
    getWall: () => undefined,
  };
  const geometry = {
    midpoint: () => ({ x: 0, y: 0 }),
    polygonCentroid: () => ({ x: 0, y: 0 }),
    segmentLength: () => 0,
    segmentNormal: () => ({ x: 0, y: 1 }),
  };
  const renderer = new Function("Model", "Geometry", `${source}; return CanvasRenderer;`)(model, geometry) as Renderer;
  renderer.init(canvas);
  return renderer;
}

describe("room planner exportPNG", () => {
  it("restores the theme and view after a fitted export", () => {
    const renderer = loadRenderer(() => "data:image/png;base64,AAAA");
    renderer.setTheme("dark");
    renderer.setZoom(2.5);
    renderer.pan(40, -30);
    const before = { zoom: renderer.getZoom(), offset: renderer.getOffset() };

    expect(renderer.exportPNG(true)).toBe("data:image/png;base64,AAAA");

    expect(renderer.getTheme()).toBe("dark");
    expect(renderer.getZoom()).toBe(before.zoom);
    expect(renderer.getOffset()).toEqual(before.offset);
  });

  it("restores the theme and view when the export throws", () => {
    const renderer = loadRenderer(() => {
      throw new DOMException("The canvas has been tainted", "SecurityError");
    });
    renderer.setTheme("dark");
    renderer.setZoom(2.5);
    renderer.pan(40, -30);
    const before = { zoom: renderer.getZoom(), offset: renderer.getOffset() };

    expect(() => renderer.exportPNG(true)).toThrow("tainted");

    expect(renderer.getTheme()).toBe("dark");
    expect(renderer.getZoom()).toBe(before.zoom);
    expect(renderer.getOffset()).toEqual(before.offset);
  });
});
