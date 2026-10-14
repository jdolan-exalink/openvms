import { describe, expect, it } from "vitest";
import { createSessionRegistry } from "./sessionRegistry";
import type { StreamStatsSource } from "./types";

const source = (): StreamStatsSource => ({ streamStats: () => ({ cameraId: "c", quality: "sub", state: "ACTIVE", bytesReceived: 0, reconnects: 0, stalls: 0 }) });

describe("session registry", () => {
  it("lists registered sources and forgets unregistered ones", () => {
    const registry = createSessionRegistry();
    const a = source();
    const b = source();
    registry.register(a);
    registry.register(b);
    expect(registry.list()).toEqual([a, b]);
    registry.unregister(a);
    expect(registry.list()).toEqual([b]);
  });

  it("ignores duplicate registrations and unknown unregistrations", () => {
    const registry = createSessionRegistry();
    const a = source();
    registry.register(a);
    registry.register(a);
    registry.unregister(source());
    expect(registry.list()).toHaveLength(1);
  });

  it("returns a copy so callers cannot mutate the registry", () => {
    const registry = createSessionRegistry();
    registry.register(source());
    (registry.list() as StreamStatsSource[]).length = 0;
    expect(registry.list()).toHaveLength(1);
  });
});
