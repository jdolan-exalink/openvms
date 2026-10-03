/**
 * liveRec holds the pure logic of the Live LIVE/REC mode (LV-9): URL state, which tiles get a
 * recorded player, master selection, event stepping and day changes that keep the time of
 * day. No React or DOM.
 */

/** Simultaneous recorded players in REC; further tiles show a snapshot and a notice. */
export const REC_MAX_PLAYERS = 16;
export const REC_LIMIT_NOTICE = `Reproducción limitada a ${REC_MAX_PLAYERS} cámaras`;
export const REC_SPEEDS = [0.5, 1, 2, 4, 8] as const;
/** Recordings are addressable this far behind now; closer than this the file may not exist yet. */
export const REC_LIVE_EDGE_S = 30;
/** Entering GRABADO starts playback this far before the current time and plays from there. */
export const REC_ENTRY_OFFSET_S = 5 * 60;

export const startOfLocalDay = (unix: number): number => {
  const d = new Date(unix * 1000);
  d.setHours(0, 0, 0, 0);
  return Math.floor(d.getTime() / 1000);
};

/** parseRecSearch reads `?mode=rec&t=<ISO|unix>`; anything else means LIVE. */
export function parseRecSearch(search: Record<string, unknown>): { rec: boolean; t?: number } {
  if (search.mode !== "rec") return { rec: false };
  const raw = search.t;
  let t: number | undefined;
  if (typeof raw === "number" && Number.isFinite(raw)) t = Math.floor(raw);
  else if (typeof raw === "string" && raw) {
    const ms = /^\d+$/.test(raw) ? Number(raw) * 1000 : new Date(raw).getTime();
    if (Number.isFinite(ms)) t = Math.floor(ms / 1000);
  }
  return { rec: true, t };
}

/** recSearch builds the search params for a mode; LIVE clears both keys. */
export function recSearch(rec: boolean, t?: number): { mode?: "rec"; t?: string } {
  if (!rec) return { mode: undefined, t: undefined };
  return { mode: "rec", t: t === undefined ? undefined : new Date(t * 1000).toISOString() };
}

/**
 * assignRecPlayers returns the camera ids that get a recorded player: the first `cap` distinct
 * cameras in tile order among those that may play; the rest are `limited`.
 */
export function assignRecPlayers(cameraIds: string[], playable: (id: string) => boolean, cap = REC_MAX_PLAYERS): { players: string[]; limited: Set<string> } {
  const players: string[] = [];
  const limited = new Set<string>();
  for (const id of new Set(cameraIds)) {
    if (!playable(id)) continue;
    if (players.length < cap) players.push(id);
    else limited.add(id);
  }
  return { players, limited };
}

/** pickMaster prefers the selected tile's camera when it can play, else the first that can. */
export function pickMaster(players: string[], selected: string | undefined, hasCoverage: (id: string) => boolean): string {
  if (selected && players.includes(selected) && hasCoverage(selected)) return selected;
  return players.find(hasCoverage) ?? players[0] ?? "";
}

/** stepEvent finds the closest event strictly after (dir 1) or before (dir -1) `from`; times are sorted. */
export function stepEvent(times: number[], from: number, dir: 1 | -1, epsilon = 1): number | undefined {
  if (dir === 1) return times.find((t) => t > from + epsilon);
  for (let i = times.length - 1; i >= 0; i--) if (times[i]! < from - epsilon) return times[i];
  return undefined;
}

/** moveToDay keeps the local time of day of `instant` on another day (clamped to `latest`). */
export function moveToDay(instant: number, day: Date, latest?: number): number {
  const from = new Date(instant * 1000);
  const moved = new Date(day.getFullYear(), day.getMonth(), day.getDate(), from.getHours(), from.getMinutes(), from.getSeconds());
  const t = Math.floor(moved.getTime() / 1000);
  return latest !== undefined ? Math.min(t, latest) : t;
}
