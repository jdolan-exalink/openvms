/**
 * The placement draft holds everything an operator changed on the map before anything is
 * written. It is a plain value tree: every action returns a new state, so undo/redo are
 * snapshots of the pending entries and React can render straight from state.
 *
 * `revisions` remembers, per camera, the placement revision the client last knew about
 * (from the site entities read or from its own save). That is the token sent back as
 * If-Match; a camera nobody ever saved has none, because a missing placement must be
 * created without the header.
 */
export interface DraftPlacement {
  entityId: string;
  entityType: "camera" | "server" | "device";
  siteId: string;
  lat: number;
  lng: number;
  bearingDeg: number;
  fovDeg: number;
  rangeM: number;
  cameraType?: "fixed" | "dome" | "ptz" | "fisheye" | "lpr";
  ptz?: boolean;
  lpr?: boolean;
}

/** A pending change paired with the revision its save has to match. */
export interface PendingPlacement extends DraftPlacement {
  revision?: number;
}

export interface DraftState {
  entries: Record<string, DraftPlacement>;
  revisions: Record<string, number>;
  conflicts: string[];
  past: Array<Record<string, DraftPlacement>>;
  future: Array<Record<string, DraftPlacement>>;
}

/** Client defaults mirroring the backend read defaults (fov 70, range 30). */
export const DEFAULT_PLACEMENT = { bearingDeg: 0, fovDeg: 70, rangeM: 30 } as const;

export function emptyDraft(): DraftState {
  return { entries: {}, revisions: {}, conflicts: [], past: [], future: [] };
}

function clone(state: DraftState): DraftState {
  return {
    entries: { ...state.entries },
    revisions: { ...state.revisions },
    conflicts: [...state.conflicts],
    past: [...state.past],
    future: [...state.future],
  };
}

/** stagePlacement records (or refines) one pending change as a single undo step. */
export function stagePlacement(state: DraftState, entry: DraftPlacement, revision?: number): DraftState {
  const next = clone(state);
  next.past.push({ ...state.entries });
  next.future = [];
  next.entries = { ...next.entries, [entry.entityId]: entry };
  if (revision !== undefined) next.revisions[entry.entityId] = revision;
  return next;
}

/** stageMany applies a bulk change (place-all-at-centre) as one undo step. */
export function stageMany(state: DraftState, entries: readonly DraftPlacement[]): DraftState {
  if (entries.length === 0) return state;
  const next = clone(state);
  next.past.push({ ...state.entries });
  next.future = [];
  for (const entry of entries) next.entries[entry.entityId] = entry;
  return next;
}

export function canUndo(state: DraftState): boolean {
  return state.past.length > 0;
}

export function canRedo(state: DraftState): boolean {
  return state.future.length > 0;
}

export function undoDraft(state: DraftState): DraftState {
  const previous = state.past[state.past.length - 1];
  if (!previous) return state;
  const next = clone(state);
  next.past = state.past.slice(0, -1);
  next.future.push({ ...state.entries });
  next.entries = { ...previous };
  return next;
}

export function redoDraft(state: DraftState): DraftState {
  const following = state.future[state.future.length - 1];
  if (!following) return state;
  const next = clone(state);
  next.future = state.future.slice(0, -1);
  next.past.push({ ...state.entries });
  next.entries = { ...following };
  return next;
}

/** pendingPlacements pairs each change with the If-Match token its save must carry. */
export function pendingPlacements(state: DraftState): PendingPlacement[] {
  return Object.values(state.entries).map((entry) => ({
    ...entry,
    revision: state.revisions[entry.entityId],
  }));
}

/** commitPlacement drops a saved change and remembers the revision the server returned. */
export function commitPlacement(state: DraftState, entityId: string, revision: number): DraftState {
  const next = clone(state);
  delete next.entries[entityId];
  next.revisions[entityId] = revision;
  next.conflicts = next.conflicts.filter((id) => id !== entityId);
  return next;
}

/** markConflict parks a 409 until the operator rebases the draft on the server revision. */
export function markConflict(state: DraftState, entityId: string): DraftState {
  if (state.conflicts.includes(entityId)) return state;
  const next = clone(state);
  next.conflicts = [...state.conflicts, entityId];
  return next;
}

/**
 * rebasePlacement adopts the fresh server revision after a conflict (or forgets the token
 * entirely when none is known), so the retry either passes the lock or writes deliberately.
 */
export function rebasePlacement(state: DraftState, entityId: string, revision?: number): DraftState {
  const next = clone(state);
  next.conflicts = next.conflicts.filter((id) => id !== entityId);
  if (revision === undefined) delete next.revisions[entityId];
  else next.revisions[entityId] = revision;
  return next;
}

/** stepBearing rotates by a signed delta, wrapping around the compass. */
export function stepBearing(current: number, delta: number): number {
  const wrapped = (current + delta) % 360;
  return wrapped < 0 ? wrapped + 360 : wrapped;
}
