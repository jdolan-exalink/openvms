import { describe, expect, it } from "vitest";
import { IncidentFocus, groupSiteHealth } from "./incidentPolicy";
import type { CameraEntity } from "./types";

const camera: CameraEntity = { id: "c", type: "camera", siteId: "s", serverId: "srv", name: "North",
  position: { kind: "geo", lat: 0, lng: 0 }, status: "offline", activeAlarms: 0, metadata: {},
  camera: { bearingDeg: 0, fovDeg: 60, rangeM: 100, cameraType: "fixed", ptz: false, lpr: false } };
describe("incident policy", () => {
  it("groups server outages once instead of creating camera outage incidents", () => {
    const groups = groupSiteHealth([{ ...camera, metadata: { serverOffline: true } },
      { ...camera, id: "other", metadata: { serverOffline: true } }, { ...camera, id: "standalone", serverId: "up" }]);
    expect(groups.map(group => group.cameraIds.length)).toEqual([2, 1]);
    expect(groups[0]!.kind).toBe("server");
  });
  it("defaults to NONE and limits opt-in to the current site", () => {
    const focus = new IncidentFocus();
    expect(focus.accept(camera, "s", 20_000)).toBe(false);
    focus.mode = "current-site";
    expect(focus.accept(camera, "unrelated", 20_000)).toBe(false);
    expect(focus.accept(camera, "s", 20_000)).toBe(true);
    expect(focus.accept(camera, "s", 25_000)).toBe(false);
  });
  it("suppresses movements for 15 seconds after manual navigation", () => {
    const focus = new IncidentFocus();
    focus.mode = "current-site";
    focus.interact(20_000);
    expect(focus.accept(camera, "s", 34_999)).toBe(false);
    expect(focus.accept(camera, "s", 35_000)).toBe(true);
  });
});
