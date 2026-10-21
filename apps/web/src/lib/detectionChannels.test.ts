import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import {
  channelKindOf, faceChannelItems, FACES_CHANNEL_ID, LPR_CHANNEL_ID, lprChannelItems,
} from "./detectionChannels";

const read = (id: string, plate: string, seen_at: string, extra: Partial<Schemas["PlateRead"]> = {}): Schemas["PlateRead"] => ({
  id, site_id: "s", site_name: "Sitio", server_id: "sv", server_name: "Srv", camera_id: "c1", camera_name: "Acceso Norte",
  plate, plate_normalized: plate, label: "car", zones: ["entrada"], seen_at, ...extra,
});

const faceEvent = (id: string, start_time: string, sub_labels: string[] = []): Schemas["Event"] => ({
  id, tenant_id: "t", site_id: "s", site_name: "Sitio", server_id: "sv", server_name: "Srv", camera_id: "c2", camera_name: "Hall",
  remote_id: `r-${id}`, severity: "detection", labels: ["face"], sub_labels, zones: ["recepcion"], plates: [], start_time,
  reviewed: false, has_thumbnail: true, has_snapshot: false, has_preview: false, tracks: [],
} as Schemas["Event"]);

describe("channelKindOf", () => {
  it("recognizes the detection channels and nothing else", () => {
    expect(channelKindOf(LPR_CHANNEL_ID)).toBe("lpr");
    expect(channelKindOf(FACES_CHANNEL_ID)).toBe("faces");
    expect(channelKindOf("visionai:lpr")).toBeNull();
    expect(channelKindOf("visionai:general")).toBeNull();
  });
});

describe("lprChannelItems", () => {
  it("maps reads newest first with the plate crop", () => {
    const items = lprChannelItems([read("a", "AB123CD", "2026-10-21T10:00:00Z"), read("b", "AC456EF", "2026-10-21T10:00:05Z")]);
    expect(items.map((i) => i.label)).toEqual(["AC456EF", "AB123CD"]);
    expect(items[0]).toMatchObject({ key: "b", kind: "lpr", cameraName: "Acceso Norte", zone: "entrada", at: "2026-10-21T10:00:05Z" });
    expect(items[0]!.imageUrl).toBe("/media/v1/lpr/reads/b/snapshot.jpg?crop=1&quality=55");
  });
});

describe("faceChannelItems", () => {
  it("names recognized faces, marks the rest as unknown, newest first", () => {
    const items = faceChannelItems([faceEvent("e1", "2026-10-21T10:00:00Z"), faceEvent("e2", "2026-10-21T10:00:09Z", ["Juan Pérez"])]);
    expect(items.map((i) => i.label)).toEqual(["Juan Pérez", "Desconocido"]);
    expect(items[0]).toMatchObject({ key: "e2", kind: "faces", cameraName: "Hall", zone: "recepcion", recognized: true });
    expect(items[1]!.recognized).toBe(false);
    expect(items[0]!.imageUrl).toBe("/api/v1/events/e2/thumbnail");
  });
});
