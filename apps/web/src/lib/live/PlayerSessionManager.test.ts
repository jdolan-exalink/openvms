import { afterEach, describe, expect, it, vi } from "vitest";
import { PlayerSessionManager } from "./PlayerSessionManager";

describe("PlayerSessionManager explicit discard", () => {
  afterEach(() => vi.restoreAllMocks());

  it("closes after removed grid consumers release without interrupting a shared active consumer", () => {
    const manager = new PlayerSessionManager({ warmSessionTTL: 60_000 });
    const first = manager.acquire("camera-1", "sub");
    const close = vi.spyOn(first, "close");
    manager.acquire("camera-1", "sub");

    manager.discard("camera-1", "sub");
    expect(close).not.toHaveBeenCalled();
    manager.release("camera-1", "sub");
    expect(close).not.toHaveBeenCalled();
    manager.release("camera-1", "sub");
    expect(close).toHaveBeenCalledWith("user-removed");
    expect(manager.stats()).toEqual({ total: 0, active: 0, warm: 0 });
  });

  it("keeps a re-acquired session WARM after an earlier discard, instead of closing it", () => {
    const manager = new PlayerSessionManager({ warmSessionTTL: 60_000 });
    const first = manager.acquire("camera-1", "sub");
    const close = vi.spyOn(first, "close");
    manager.acquire("camera-1", "sub");

    manager.discard("camera-1", "sub");
    manager.acquire("camera-1", "sub");
    manager.release("camera-1", "sub");
    manager.release("camera-1", "sub");
    manager.release("camera-1", "sub");

    expect(close).not.toHaveBeenCalled();
    expect(manager.stats()).toEqual({ total: 1, active: 0, warm: 1 });
  });
});
