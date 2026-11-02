import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { filterFeed, mergeFeed } from "./faceFeed";

const rec = (id: string, at: string, over: Partial<Schemas["FaceRecognition"]> = {}): Schemas["FaceRecognition"] =>
  ({ event_id: id, person_id: "p1", person_name: "Ana", camera_id: "c1", camera_name: "Acceso", site_id: "s1", site_name: "Sede", server_name: "srv", start_time: at, has_thumbnail: true, has_snapshot: false, ...over }) as Schemas["FaceRecognition"];
const sight = (id: string, at: string, over: Partial<Schemas["FaceReviewSighting"]> = {}): Schemas["FaceReviewSighting"] =>
  ({ id, identity_id: id, server_id: "v1", server_name: "Servidor 1", site_id: "s1", first_seen_at: at, crop_available: true, ...over }) as Schemas["FaceReviewSighting"];

describe("mergeFeed", () => {
  it("orders known and unknown faces on one timeline, newest first", () => {
    const items = mergeFeed(
      [rec("e1", "2026-11-02T10:00:00Z"), rec("e2", "2026-11-02T10:05:00Z")],
      [sight("u1", "2026-11-02T10:03:00Z")],
    );
    expect(items.map((i) => i.key)).toEqual(["k:e2:p1", "u:u1", "k:e1:p1"]);
  });

  it("uses the camera for a recognized face and the server for an unknown one", () => {
    const [known, unknown] = mergeFeed([rec("e1", "2026-11-02T10:05:00Z")], [sight("u1", "2026-11-02T10:00:00Z")]);
    expect(known).toMatchObject({ kind: "known", name: "Ana", place: "Acceso · Sede", thumb: "/api/v1/events/e1/thumbnail" });
    expect(known!.playback).toEqual({ camera: "c1", t: Math.floor(new Date("2026-11-02T10:05:00Z").getTime() / 1000) });
    expect(unknown).toMatchObject({ kind: "unknown", name: "", place: "Servidor 1" });
    expect(unknown!.thumb).toBe("/api/v1/faces/review-inbox/u1/image");
  });

  it("prefers the time parsed from the file name and omits missing pictures", () => {
    const [item] = mergeFeed([], [sight("u1", "2026-11-02T10:00:00Z", { seen_at: "2026-11-02T09:00:00Z", crop_available: false })]);
    expect(item!.at).toBe(new Date("2026-11-02T09:00:00Z").getTime());
    expect(item!.thumb).toBeUndefined();
    expect(mergeFeed([rec("e1", "2026-11-02T10:00:00Z", { has_thumbnail: false })], [])[0]!.thumb).toBeUndefined();
  });

  it("keeps at most the requested number of items", () => {
    const many = Array.from({ length: 5 }, (_, i) => rec(`e${i}`, `2026-11-02T10:0${i}:00Z`));
    expect(mergeFeed(many, [], 3)).toHaveLength(3);
  });
});

describe("filterFeed", () => {
  const items = mergeFeed([rec("e1", "2026-11-02T10:00:00Z")], [sight("u1", "2026-11-02T10:01:00Z")]);
  it("filters by state and returns everything for all", () => {
    expect(filterFeed(items, "all")).toHaveLength(2);
    expect(filterFeed(items, "known").map((i) => i.kind)).toEqual(["known"]);
    expect(filterFeed(items, "unknown").map((i) => i.kind)).toEqual(["unknown"]);
  });
});
