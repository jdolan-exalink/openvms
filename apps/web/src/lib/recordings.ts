export const MAX_VOD_WINDOW_SECONDS = 60 * 60;

/**
 * clampToNow keeps a chosen instant from being in the future. The day timeline spans the whole day, so a click
 * on its right side picks a moment that has no recording and would ask the gateway for a range that ends
 * before it starts.
 */
export function clampToNow(instant: number, now: number): number {
  return Math.min(instant, now - 5);
}

/** Return the existing hour-sized VOD range used to play around a selected instant. */
export function vodWindowForInstant(instant: number, dayStart: number, now: number) {
  const start = Math.max(instant - 60, dayStart);
  return { start, end: Math.min(start + MAX_VOD_WINDOW_SECONDS, now) };
}
