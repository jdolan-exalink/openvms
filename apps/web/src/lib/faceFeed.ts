import type { Schemas } from "@/api/client";
import { faceReviewSightingImageUrl } from "@/api/queries";

export type FeedItem = {
  key: string;
  kind: "known" | "unknown";
  /** Milliseconds since the epoch. */
  at: number;
  /** The person's name; empty for an unknown face. */
  name: string;
  /** Camera and site for a recognized face, the server for an unknown one (a sighting has no camera). */
  place: string;
  thumb?: string;
  playback?: { camera: string; t: number };
};

type Recognition = Schemas["FaceRecognition"];
type Sighting = Schemas["FaceReviewSighting"];

/**
 * mergeFeed puts recognized and unknown faces on one timeline, newest first. A face the operator already
 * named is no longer an unknown sighting, so the two lists never repeat the same face.
 */
export function mergeFeed(recognitions: Recognition[], sightings: Sighting[], limit = 60): FeedItem[] {
  const known: FeedItem[] = recognitions.map((r) => {
    const at = new Date(r.start_time).getTime();
    return {
      key: `k:${r.event_id}:${r.person_id}`,
      kind: "known",
      at,
      name: r.person_name,
      place: `${r.camera_name} · ${r.site_name}`,
      thumb: r.has_thumbnail ? `/api/v1/events/${r.event_id}/thumbnail` : undefined,
      playback: { camera: r.camera_id, t: Math.floor(at / 1000) },
    };
  });
  const unknown: FeedItem[] = sightings.map((s) => ({
    key: `u:${s.id}`,
    kind: "unknown",
    at: new Date(s.seen_at ?? s.first_seen_at).getTime(),
    name: "",
    place: s.server_name,
    thumb: s.crop_available ? faceReviewSightingImageUrl(s.id) : undefined,
  }));
  return [...known, ...unknown].sort((a, b) => b.at - a.at || a.key.localeCompare(b.key)).slice(0, limit);
}

export type FeedFilter = "all" | "known" | "unknown";

export function filterFeed(items: FeedItem[], filter: FeedFilter): FeedItem[] {
  return filter === "all" ? items : items.filter((item) => item.kind === filter);
}
