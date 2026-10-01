import { describe, expect, it } from "vitest";
import {
  DEFAULT_PLACEMENT,
  canRedo,
  canUndo,
  commitPlacement,
  emptyDraft,
  markConflict,
  pendingPlacements,
  rebasePlacement,
  redoDraft,
  stageMany,
  stagePlacement,
  stepBearing,
  undoDraft,
  type DraftPlacement,
} from "./placementDraft";

const entry = (over: Partial<DraftPlacement> = {}): DraftPlacement => ({
  entityId: "c1",
  entityType: "camera",
  siteId: "s",
  lat: -34.6037,
  lng: -58.3816,
  ...DEFAULT_PLACEMENT,
  ...over,
});

describe("placementDraft", () => {
  it("starts with nothing pending and nothing to undo", () => {
    const draft = emptyDraft();
    expect(pendingPlacements(draft)).toEqual([]);
    expect(canUndo(draft)).toBe(false);
    expect(canRedo(draft)).toBe(false);
  });

  it("carries the revision a save has to match, when one is known", () => {
    const withRev = stagePlacement(emptyDraft(), entry(), 7);
    expect(pendingPlacements(withRev)).toEqual([expect.objectContaining({ entityId: "c1", revision: 7 })]);

    const unknown = stagePlacement(emptyDraft(), entry());
    expect(pendingPlacements(unknown)[0]?.revision).toBeUndefined();
  });

  it("collapses repeat edits of one camera into a single pending change", () => {
    let draft = stagePlacement(emptyDraft(), entry({ lat: -34.6 }), 3);
    draft = stagePlacement(draft, entry({ lat: -34.7, bearingDeg: 90 }));
    const pending = pendingPlacements(draft);
    expect(pending).toHaveLength(1);
    expect(pending[0]).toMatchObject({ lat: -34.7, bearingDeg: 90, revision: 3 });

    // Refining the same camera stays one pending entry; undo walks its values back and
    // then clears it, instead of leaving one undo step per intermediate edit behind.
    draft = undoDraft(draft);
    expect(pendingPlacements(draft)).toEqual([expect.objectContaining({ lat: -34.6, revision: 3 })]);
    draft = undoDraft(draft);
    expect(pendingPlacements(draft)).toHaveLength(0);
  });

  it("walks undo and redo, and a new edit drops the redo branch", () => {
    let draft = stagePlacement(emptyDraft(), entry({ lat: 1 }));
    draft = stagePlacement(draft, entry({ lat: 2 }));
    draft = undoDraft(draft);
    expect(pendingPlacements(draft)[0]?.lat).toBe(1);
    expect(canRedo(draft)).toBe(true);

    draft = redoDraft(draft);
    expect(pendingPlacements(draft)[0]?.lat).toBe(2);

    draft = undoDraft(draft);
    draft = stagePlacement(draft, entry({ lat: 3 }));
    expect(canRedo(draft)).toBe(false);
    expect(pendingPlacements(draft)[0]?.lat).toBe(3);

    const empty = emptyDraft();
    expect(undoDraft(empty)).toBe(empty);
    expect(redoDraft(empty)).toBe(empty);
  });

  it("commits a saved camera out of the draft and remembers the fresh revision", () => {
    let draft = stagePlacement(emptyDraft(), entry(), 1);
    draft = stagePlacement(draft, entry({ entityId: "c2" }));
    draft = commitPlacement(draft, "c1", 4);

    expect(pendingPlacements(draft).map((p) => p.entityId)).toEqual(["c2"]);
    // The next edit of c1 must send If-Match: "4".
    draft = stagePlacement(draft, entry({ lat: -34.9 }));
    expect(pendingPlacements(draft).find((p) => p.entityId === "c1")?.revision).toBe(4);
  });

  it("holds a 409 as a conflict until the draft is rebased on the server revision", () => {
    let draft = stagePlacement(emptyDraft(), entry(), 4);
    draft = markConflict(draft, "c1");
    expect(draft.conflicts).toEqual(["c1"]);

    draft = rebasePlacement(draft, "c1", 9);
    expect(draft.conflicts).toEqual([]);
    expect(pendingPlacements(draft)[0]?.revision).toBe(9);

    // Rebasing without a fresh revision drops the token: the retry must not send If-Match.
    draft = markConflict(draft, "c1");
    draft = rebasePlacement(draft, "c1");
    expect(pendingPlacements(draft)[0]?.revision).toBeUndefined();
  });

  it("treats a bulk placement as one undo step", () => {
    let draft = stageMany(emptyDraft(), [entry({ entityId: "c1" }), entry({ entityId: "c2" }), entry({ entityId: "c3" })]);
    expect(pendingPlacements(draft)).toHaveLength(3);
    draft = undoDraft(draft);
    expect(pendingPlacements(draft)).toHaveLength(0);
  });

  it("steps bearings by 15° and wraps around the compass", () => {
    expect(stepBearing(0, 15)).toBe(15);
    expect(stepBearing(350, 15)).toBe(5);
    expect(stepBearing(5, -15)).toBe(350);
    expect(stepBearing(0, -15)).toBe(345);
    expect(stepBearing(0, 10)).toBe(10); // free rotation stays exact
  });
});
