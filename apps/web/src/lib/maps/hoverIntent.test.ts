import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HoverIntentManager } from "./hoverIntent";

describe("HoverIntentManager", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("cascades through stages 0ms -> 150ms -> 400ms -> 700ms", () => {
    const onPrewarm = vi.fn();
    const onRelease = vi.fn();
    const manager = new HoverIntentManager({
      onPrewarm,
      onRelease,
      liveOnHover: true,
    });

    manager.enter("cam-1", 100, 200);

    // 0ms: tooltip
    expect(manager.getState().stage).toBe("tooltip");
    expect(manager.getState().cameraId).toBe("cam-1");
    expect(manager.getState().x).toBe(100);
    expect(manager.getState().y).toBe(200);

    // 150ms: snapshot
    vi.advanceTimersByTime(150);
    expect(manager.getState().stage).toBe("snapshot");

    // 400ms: prewarm
    vi.advanceTimersByTime(250);
    expect(manager.getState().stage).toBe("prewarm");
    expect(onPrewarm).toHaveBeenCalledWith("cam-1");

    // 700ms: live
    vi.advanceTimersByTime(300);
    expect(manager.getState().stage).toBe("live");

    // leave: resets and calls onRelease
    manager.leave();
    expect(manager.getState().stage).toBe("none");
    expect(manager.getState().cameraId).toBeNull();
    expect(onRelease).toHaveBeenCalledWith("cam-1");

    manager.destroy();
  });

  it("stops at prewarm if liveOnHover is false", () => {
    const manager = new HoverIntentManager({
      liveOnHover: false,
    });

    manager.enter("cam-2");
    vi.advanceTimersByTime(1000);
    expect(manager.getState().stage).toBe("prewarm");

    manager.destroy();
  });

  it("releases previous target immediately when entering new target", () => {
    const onRelease = vi.fn();
    const manager = new HoverIntentManager({ onRelease });

    manager.enter("cam-a");
    vi.advanceTimersByTime(200);

    manager.enter("cam-b");
    expect(onRelease).toHaveBeenCalledWith("cam-a");
    expect(manager.getState().cameraId).toBe("cam-b");
    expect(manager.getState().stage).toBe("tooltip");

    manager.destroy();
  });
});
