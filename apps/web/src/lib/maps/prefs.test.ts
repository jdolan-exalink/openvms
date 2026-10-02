import { afterEach, describe, expect, it, vi } from "vitest";
import { json } from "@/test-utils";
import { applyFilters, fetchMapUserPrefs, mergeLayers, saveMapUserPrefs } from "./prefs";
import { DEFAULT_LAYER_PREFERENCE, type CameraEntity, type MapFilters } from "./types";

const camera = (over: Partial<CameraEntity> = {}): CameraEntity => ({
  id: "cam-1",
  type: "camera",
  siteId: "site-1",
  serverId: "srv-1",
  name: "Acceso norte",
  status: "online",
  activeAlarms: 0,
  position: { kind: "geo", lat: -34.6, lng: -58.3 },
  camera: { bearingDeg: 0, fovDeg: 60, rangeM: 100, cameraType: "fixed", ptz: false, lpr: false },
  metadata: {},
  ...over,
});

describe("mergeLayers", () => {
  it("falls back to the documented defaults for absent keys", () => {
    expect(mergeLayers()).toEqual(DEFAULT_LAYER_PREFERENCE);
    expect(mergeLayers({})).toEqual(DEFAULT_LAYER_PREFERENCE);
  });

  it("overrides only the keys the user actually saved", () => {
    const merged = mergeLayers({ coverage: false, heatmap: true });
    expect(merged.coverage).toBe(false);
    expect(merged.heatmap).toBe(true);
    expect(merged.cameras).toBe(DEFAULT_LAYER_PREFERENCE.cameras);
  });
});

describe("applyFilters", () => {
  const cameras = [
    camera({ id: "ok" }),
    camera({ id: "alarm", activeAlarms: 2 }),
    camera({ id: "down", status: "offline" }),
    camera({ id: "ptz", camera: { bearingDeg: 0, fovDeg: 60, rangeM: 100, cameraType: "ptz", ptz: true, lpr: false } }),
    camera({ id: "other-site", siteId: "site-2" }),
    camera({ id: "cam-2", serverId: "srv-2" }),
  ];

  it("returns everything when no filter is set", () => {
    expect(applyFilters(cameras, {})).toHaveLength(cameras.length);
    expect(applyFilters(cameras, undefined)).toHaveLength(cameras.length);
  });

  it("ignores legacy status and priority exclusions without changing explicit dimensions", () => {
    const inventory = Array.from({ length: 26 }, (_, i) => camera({
      id: `camera-${i}`, status: i < 24 ? "online" : "unknown", activeAlarms: i < 20 ? 1 : 0,
    }));
    const legacy: MapFilters = { status: ["ONLINE"], priority: ["alert"] };
    expect(applyFilters(inventory, legacy)).toHaveLength(26);
    expect(legacy).toEqual({ status: ["ONLINE"], priority: ["alert"] });
    expect(applyFilters(cameras, { status: ["OFFLINE"], camera_types: ["ptz"] }).map(c => c.id)).toEqual(["ptz"]);
    const serverDown = camera({ metadata: { serverOffline: true } });
    expect(applyFilters([serverDown], { status: ["ONLINE"] })).toEqual([serverDown]);
  });

  it("restricts by site, camera and server when those are set", () => {
    expect(applyFilters(cameras, { site_ids: ["site-1"] }).map((c) => c.id)).not.toContain("other-site");
    expect(applyFilters(cameras, { camera_ids: ["cam-2"] }).map((c) => c.id)).toEqual(["cam-2"]);
    expect(applyFilters(cameras, { server_ids: ["srv-2"] }).map((c) => c.id)).toEqual(["cam-2"]);
  });
});

describe("map preferences transport", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("reads whatever the user saved", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => json({ layers: { coverage: false }, focus_mode: "current-site" })));
    const prefs = await fetchMapUserPrefs();
    expect(prefs.layers?.coverage).toBe(false);
    expect(prefs.focus_mode).toBe("current-site");
  });

  it("treats a user who never saved anything as an empty preference set", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => json({})));
    const prefs = await fetchMapUserPrefs();
    expect(prefs).toEqual({});
  });

  it("replaces the whole blob instead of merging it", async () => {
    const calls: { url: string; body: unknown }[] = [];
    vi.stubGlobal("fetch", vi.fn(async (input: Request) => {
      const raw = await input.text();
      calls.push({
        url: new URL(input.url).pathname + new URL(input.url).search,
        body: raw ? JSON.parse(raw) : undefined,
      });
      return json({ layers: { cameras: false } });
    }));
    const saved = await saveMapUserPrefs({ layers: { cameras: false } });
    expect(calls).toHaveLength(1);
    expect(calls[0]!.url).toBe("/api/v1/me/map-prefs");
    expect(calls[0]!.body).toEqual({ layers: { cameras: false } });
    expect(saved.layers?.cameras).toBe(false);
  });

  it("surfaces the server's rejection of an oversized blob", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => json({ code: "invalid", message: "map preferences exceed 16384 bytes" }, 400)));
    await expect(saveMapUserPrefs({ filters: { tags: ["x"] } satisfies MapFilters }))
      .rejects.toThrow("map preferences exceed 16384 bytes");
  });
});
