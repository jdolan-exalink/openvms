import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { pinnedWindowsKey, savePinnedWindows } from "@/lib/maps/pinnedWindows";
import { usePinnedMapWindows } from "./usePinnedMapWindows";

afterEach(() => localStorage.clear());

describe("usePinnedMapWindows", () => {
  it("does not expose or persist another map's windows while the scope changes", () => {
    const firstKey = pinnedWindowsKey("tenant", "user", "geo:first");
    const secondKey = pinnedWindowsKey("tenant", "user", "geo:second");
    savePinnedWindows(firstKey, [{ id: "first-camera", x: 10, y: 20 }]);
    savePinnedWindows(secondKey, [{ id: "second-camera", x: 30, y: 40 }]);

    const { result, rerender } = renderHook(
      ({ scope }) => usePinnedMapWindows("tenant", "user", scope),
      { initialProps: { scope: "geo:first" } },
    );
    expect(result.current.windows.map((window) => window.id)).toEqual(["first-camera"]);

    rerender({ scope: "geo:second" });
    expect(result.current.windows.map((window) => window.id)).toEqual(["second-camera"]);
    expect(JSON.parse(localStorage.getItem(secondKey) ?? "[]")).toEqual([
      { id: "second-camera", x: 30, y: 40 },
    ]);

    act(() => result.current.pin("another-camera"));
    expect(result.current.windows.map((window) => window.id)).toEqual(["second-camera", "another-camera"]);
    expect(JSON.parse(localStorage.getItem(firstKey) ?? "[]")).toEqual([
      { id: "first-camera", x: 10, y: 20 },
    ]);
  });

  it("clears visible windows when the signed-in user becomes unavailable", () => {
    const key = pinnedWindowsKey("tenant", "user", "geo:first");
    savePinnedWindows(key, [{ id: "private-camera", x: 10, y: 20 }]);
    const { result, rerender } = renderHook(
      ({ userId }: { userId: string | undefined }) => usePinnedMapWindows("tenant", userId, "geo:first"),
      { initialProps: { userId: "user" as string | undefined } },
    );
    expect(result.current.windows.map((window) => window.id)).toEqual(["private-camera"]);
    rerender({ userId: undefined });
    expect(result.current.ready).toBe(false);
    expect(result.current.windows).toEqual([]);
  });
});
