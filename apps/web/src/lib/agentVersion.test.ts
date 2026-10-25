import { describe, expect, it } from "vitest";
import { isNewerAgentVersion } from "./agentVersion";

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
