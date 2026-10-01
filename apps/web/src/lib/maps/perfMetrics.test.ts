import { describe, expect, it } from "vitest";
import { createPerfMetrics, installPerfMetrics, perfMetricsEnabled } from "./perfMetrics";

describe("createPerfMetrics", () => {
  it("derives fps from a rolling window of frame deltas", () => {
    const metrics = createPerfMetrics();
    let now = 0;
    for (let i = 0; i < 40; i++) {
      now += 20; // 50 fps
      metrics.sample(now);
    }
    expect(metrics.snapshot().fps).toBeCloseTo(50, 0);
  });

  it("reports zero fps before two samples and recovers after slow frames", () => {
    const metrics = createPerfMetrics();
    metrics.sample(0);
    expect(metrics.snapshot().fps).toBe(0);

    let now = 0;
    for (let i = 0; i < 10; i++) {
      now += 100; // 10 fps stretch
      metrics.sample(now);
    }
    for (let i = 0; i < 40; i++) {
      now += 16; // healthy stretch wins the window
      metrics.sample(now);
    }
    expect(metrics.snapshot().fps).toBeGreaterThan(45);
  });

  it("tracks entities visible and marks the first render once", () => {
    let now = 1000;
    const metrics = createPerfMetrics({ now: () => now });
    metrics.setEntitiesVisible(0);
    metrics.markFirstRender(); // nothing rendered yet: ignored
    expect(metrics.snapshot().timeToFirstRenderMs).toBeNull();

    metrics.setEntitiesVisible(5000);
    metrics.markFirstRender();
    expect(metrics.snapshot().entitiesVisible).toBe(5000);
    expect(metrics.snapshot().timeToFirstRenderMs).toBe(0);

    now += 250;
    metrics.markFirstRender(); // a second call must not move the marker
    expect(metrics.snapshot().timeToFirstRenderMs).toBe(0);
  });

  it("counts events per second over a one second window", () => {
    let now = 0;
    const metrics = createPerfMetrics({ now: () => now });
    for (let i = 0; i < 50; i++) metrics.recordEventsApplied(1);
    expect(metrics.snapshot().eventsPerSec).toBe(50);

    now += 1500; // the whole window aged out
    expect(metrics.snapshot().eventsPerSec).toBe(0);
  });

  it("records the latest websocket lag", () => {
    const metrics = createPerfMetrics();
    metrics.recordWsLag(120);
    metrics.recordWsLag(45);
    expect(metrics.snapshot().wsLagMs).toBe(45);
  });

  it("starts the long-task budget once the render loop settles", () => {
    let now = 1000;
    const metrics = createPerfMetrics({ now: () => now });
    metrics.setEntitiesVisible(5000);
    metrics.markFirstRender();

    metrics.recordLongTask(900, 80); // init work before the first render
    expect(metrics.snapshot().maxLongTaskMs).toBe(0);

    now = 1500;
    metrics.markRenderSettled();
    metrics.recordLongTask(1400, 63); // started before idle: still the init tail
    expect(metrics.snapshot().maxLongTaskMs).toBe(0);

    metrics.recordLongTask(1500, 30);
    metrics.recordLongTask(1700, 80);
    expect(metrics.snapshot().maxLongTaskMs).toBe(80);
  });
});

describe("perf overlay install", () => {
  it("exposes the live snapshot under window.__openvmsMapMetrics", () => {
    const target = {} as Window & { __openvmsMapMetrics?: unknown };
    const { stop } = installPerfMetrics(target, { raf: () => () => {} });
    expect(target.__openvmsMapMetrics).toBeTypeOf("object");
    stop();
  });

  it("stops sampling after stop()", () => {
    const target = {} as Window & { __openvmsMapMetrics?: unknown };
    let ticking = 0;
    const { stop } = installPerfMetrics(target, {
      raf: () => {
        ticking++;
        return () => {}; // never re-arms: static frame source
      },
    });
    stop();
    expect(ticking).toBe(1);
  });

  it("gates collection on dev mode or the ?perf param", () => {
    expect(perfMetricsEnabled("", false)).toBe(false);
    expect(perfMetricsEnabled("?site=s", false)).toBe(false);
    expect(perfMetricsEnabled("?site=s&perf=1", false)).toBe(true);
    expect(perfMetricsEnabled("", true)).toBe(true);
  });
});
