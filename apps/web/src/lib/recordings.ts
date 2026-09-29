export const MAX_VOD_WINDOW_SECONDS = 60 * 60;

/** Return the existing hour-sized VOD range used to play around a selected instant. */
export function vodWindowForInstant(instant: number, dayStart: number, now: number) {
  const start = Math.max(instant - 60, dayStart);
  return { start, end: Math.min(start + MAX_VOD_WINDOW_SECONDS, now) };
}
