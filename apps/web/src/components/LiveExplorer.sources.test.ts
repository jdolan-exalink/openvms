import { describe, expect, it } from "vitest";
import { FACES_CHANNEL_ID, LPR_CHANNEL_ID } from "@/lib/detectionChannels";
import { VIRTUAL_VISION_SOURCES } from "./LiveExplorer";

describe("virtual vision sources", () => {
  it("lists the LPR and face channels as totem sources", () => {
    const totems = VIRTUAL_VISION_SOURCES.filter((s) => s.layout === "tótem");
    expect(totems.map((s) => [s.id, s.name])).toEqual([
      [LPR_CHANNEL_ID, "Tótem LPR"],
      [FACES_CHANNEL_ID, "Tótem Rostros"],
    ]);
  });
});
