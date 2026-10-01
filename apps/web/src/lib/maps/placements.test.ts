import { afterEach, describe, expect, it, vi } from "vitest";
import { json } from "@/test-utils";
import { DEFAULT_PLACEMENT, type DraftPlacement } from "./placementDraft";
import { savePlacements } from "./placements";

const entry = (over: Partial<DraftPlacement & { revision?: number }> = {}): DraftPlacement & { revision?: number } => ({
  entityId: "c1",
  entityType: "camera",
  siteId: "s",
  lat: -34.6037,
  lng: -58.3816,
  ...DEFAULT_PLACEMENT,
  ...over,
});

const placementPut = (fetchSpy: { mock: { calls: unknown[][] } }) =>
  fetchSpy.mock.calls
    .map(([input]) => input as Request)
    .filter((request) => request && typeof request === "object" && request.method === "PUT"
      && new URL(request.url).pathname === "/api/v1/maps/placements/camera/c1");

afterEach(() => vi.unstubAllGlobals());

describe("savePlacements", () => {
  it("sends the known revision as If-Match and reads back the new one", async () => {
    const fetchSpy = vi.fn(async () => json({ id: "p1", revision: 5 }));
    vi.stubGlobal("fetch", fetchSpy);

    const outcome = await savePlacements([entry({ revision: 4 })]);

    expect(outcome).toEqual({ saved: [{ entityId: "c1", revision: 5 }], conflicts: [], failed: [] });
    const [request] = placementPut(fetchSpy);
    expect(request!.headers.get("If-Match")).toBe(`"4"`);
    const body = JSON.parse(await request!.text()) as Record<string, unknown>;
    expect(body).toMatchObject({ site_id: "s", lat: -34.6037, lng: -58.3816, bearing_deg: 0, fov_deg: 70, range_m: 30 });
  });

  it("never sends If-Match for a camera nobody saved before", async () => {
    const fetchSpy = vi.fn(async () => json({ id: "p1", revision: 1 }));
    vi.stubGlobal("fetch", fetchSpy);

    await savePlacements([entry()]);

    expect(placementPut(fetchSpy)[0]!.headers.get("If-Match")).toBeNull();
  });

  it("reports a stale write as a conflict instead of a failure", async () => {
    const fetchSpy = vi.fn(async () => json({ code: "conflict", message: "stale" }, 409));
    vi.stubGlobal("fetch", fetchSpy);

    const outcome = await savePlacements([entry({ revision: 4 })]);

    expect(outcome).toEqual({ saved: [], conflicts: [{ entityId: "c1", message: "stale" }], failed: [] });
  });

  it("keeps going and surfaces other errors per camera", async () => {
    const fetchSpy = vi.fn(async (request: Request) =>
      new URL(request.url).pathname.endsWith("/c1")
        ? json({ code: "forbidden", message: "sin permiso" }, 403)
        : json({ id: "p2", revision: 2 }));
    vi.stubGlobal("fetch", fetchSpy);

    const outcome = await savePlacements([entry(), entry({ entityId: "c2" })]);

    expect(outcome.saved).toEqual([{ entityId: "c2", revision: 2 }]);
    expect(outcome.failed).toEqual([{ entityId: "c1", message: "sin permiso" }]);
  });
});
