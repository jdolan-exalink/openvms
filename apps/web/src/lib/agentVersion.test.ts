import { describe, expect, it } from "vitest";
import { isAtLeastAgentVersion, isNewerAgentVersion } from "./agentVersion";

describe("isNewerAgentVersion", () => {
  it.each([
    ["0.4.3", "0.4.0", true],
    ["0.10.0", "0.4.9", true],
    ["v0.4.3", "0.4.0", true],
    ["0.4.0", "0.4.3", false],
    ["0.4.3", "0.4.3", false],
    ["dev", "0.4.0", false],
    ["0.4.3", undefined, false],
    [undefined, "0.4.0", false],
  ])("%s newer than %s is %s", (version, other, want) => {
    expect(isNewerAgentVersion(version, other)).toBe(want);
  });
});

describe("isAtLeastAgentVersion", () => {
  it.each([
    ["0.4.4", "0.4.4", true],
    ["0.4.5", "0.4.4", true],
    ["0.10.0", "0.4.9", true],
    ["0.4.3", "0.4.4", false],
    ["dev", "0.4.4", false],
    ["0.4.4", "dev", false],
    [undefined, "0.4.4", false],
    ["0.4.4", undefined, false],
  ])("%s at least %s is %s", (version, other, want) => {
    expect(isAtLeastAgentVersion(version, other)).toBe(want);
  });
});
