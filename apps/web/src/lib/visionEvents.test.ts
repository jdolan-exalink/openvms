import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { eventToView, plateToView, slotToView } from "./visionEvents";

const slot = (extra: Partial<Schemas["VisionAISlot"]> = {}): Schemas["VisionAISlot"] => ({
  slot: 0, camera_id: "cam-1", pinned: false, camera_name: "Acceso", object: "car", confidence: 0.91, zone: "entrada",
  event_id: "ev-1", plate: "AB123CD", assigned_at: "2026-10-21T10:00:00Z", ...extra,
});

const event = (extra: Partial<Schemas["Event"]> = {}): Schemas["Event"] => ({
  id: "ev-2", tenant_id: "t", site_id: "s", site_name: "Sitio", server_id: "sv", server_name: "Srv", camera_id: "cam-2",
  camera_name: "Patio", remote_id: "r", severity: "detection", labels: ["person"], sub_labels: [], zones: ["jardin"], plates: [],
  start_time: "2026-10-21T09:00:00Z", reviewed: false, has_thumbnail: true, has_snapshot: true, has_preview: false, tracks: [],
  ...extra,
});

describe("slotToView", () => {
  it("keeps the slot data and its plate", () => {
    const view = slotToView(slot(), true);
    expect(view).toMatchObject({ source: "slot", cameraName: "Acceso", object: "car", plate: "AB123CD", zone: "entrada", at: "2026-10-21T10:00:00Z" });
    expect(view.confidence).toBeCloseTo(0.91);
    expect(view.slot).toBeDefined();
  });

  it("prefers the snapshot and falls back to the thumbnail, or to a thumbnail only without permission", () => {
    expect(slotToView(slot(), true).images).toEqual(["/media/v1/events/ev-1/snapshot.jpg", "/api/v1/events/ev-1/thumbnail"]);
    expect(slotToView(slot(), false).images).toEqual(["/api/v1/events/ev-1/thumbnail"]);
  });

  it("has no images without an event id", () => {
    expect(slotToView(slot({ event_id: undefined }), true).images).toEqual([]);
  });
});

describe("eventToView", () => {
  it("uses the first label as the object and the first plate", () => {
    const view = eventToView(event({ labels: ["car"], plates: ["AE526KV"] }), true);
    expect(view).toMatchObject({ source: "event", cameraName: "Patio", object: "car", plate: "AE526KV", zone: "jardin", at: "2026-10-21T09:00:00Z" });
  });

  it("only offers images the event actually has", () => {
    expect(eventToView(event({ has_snapshot: false, has_thumbnail: true }), true).images).toEqual(["/api/v1/events/ev-2/thumbnail"]);
    expect(eventToView(event({ has_snapshot: false, has_thumbnail: false }), true).images).toEqual([]);
  });

  it("shows the recognized name of a face event", () => {
    expect(eventToView(event({ labels: ["face"], sub_labels: ["", "Ana"] }), true).person).toBe("Ana");
    expect(eventToView(event({ labels: ["face"], sub_labels: [] }), true).person).toBeUndefined();
  });
});

describe("plateToView", () => {
  it("maps a plate read with its crop as the last fallback", () => {
    const read = {
      id: "pr-1", site_id: "s", site_name: "S", server_id: "sv", server_name: "Srv", camera_id: "cam-3", camera_name: "Garita",
      plate: "AB123CD", plate_normalized: "AB123CD", label: "car", zones: ["acceso"], seen_at: "2026-10-21T08:00:00Z", score: 0.8,
    } as Schemas["PlateRead"];
    const view = plateToView(read, true);
    expect(view).toMatchObject({ source: "plate", cameraName: "Garita", plate: "AB123CD", object: "car", zone: "acceso", at: "2026-10-21T08:00:00Z" });
    expect(view.images).toEqual(["/media/v1/lpr/reads/pr-1/snapshot.jpg", "/media/v1/lpr/reads/pr-1/snapshot.jpg?crop=1&quality=55"]);
    expect(plateToView(read, false).images).toEqual(["/media/v1/lpr/reads/pr-1/snapshot.jpg?crop=1&quality=55"]);
  });
});
