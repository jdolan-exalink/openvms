import { describe, expect, it } from "vitest";
import { decideSync, DRIFT_HARD_SEEK_S, NUDGE_FACTOR, type FollowerState } from "./playbackSync";

const ok: FollowerState = { currentTime: 10, playbackRate: 1, paused: false, seeking: false, readyState: 4 };

describe("decideSync", () => {
  it("does nothing within tolerance", () => {
    expect(decideSync(10.05, 1, ok)).toEqual({ kind: "none" });
  });
  it("slows a follower that is ahead and speeds one that is behind", () => {
    expect(decideSync(9.5, 1, ok)).toEqual({ kind: "rate", rate: 1 - NUDGE_FACTOR });
    expect(decideSync(10.5, 1, ok)).toEqual({ kind: "rate", rate: 1 + NUDGE_FACTOR });
  });
  it("nudges around the selected speed", () => {
    expect(decideSync(10.5, 2, ok)).toEqual({ kind: "rate", rate: 2 * (1 + NUDGE_FACTOR) });
  });
  it("hard seeks on large drift", () => {
    expect(decideSync(10 + DRIFT_HARD_SEEK_S + 0.5, 1, ok)).toEqual({ kind: "seek", time: 11.5 });
  });
  it("restores the speed once settled", () => {
    expect(decideSync(10.01, 1, { ...ok, playbackRate: 1.05 })).toEqual({ kind: "rate", rate: 1 });
  });
  it("does not fight buffering followers", () => {
    expect(decideSync(15, 1, { ...ok, readyState: 2 })).toEqual({ kind: "none" });
    expect(decideSync(15, 1, { ...ok, unavailable: true })).toEqual({ kind: "none" });
  });
});
