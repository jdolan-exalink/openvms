import type { Quality } from "@/lib/liveGrid";

/** True for H.265 under either name; go2rtc reports "h265", ONVIF and browsers say "hevc". */
export function isH265(codec: string | null | undefined): boolean {
  const c = codec?.trim().toLowerCase();
  return c === "h265" || c === "hevc";
}

/** The codec of the stream a tile is playing: the live (sub) one in the grid, the hq (main) one expanded. */
export function streamCodec(
  camera: { live_codec?: string | null; hq_codec?: string | null } | undefined,
  quality: Quality,
): string | undefined {
  return (quality === "main" ? camera?.hq_codec : camera?.live_codec) ?? undefined;
}
