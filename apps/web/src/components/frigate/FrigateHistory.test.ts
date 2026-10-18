import { describe, expect, it } from "vitest";
import { kindLabel } from "./FrigateHistory";

describe("Frigate config revision labels", () => {
  it("labels server-wide configuration patches", () => {
    expect(kindLabel.server_patch).toBe("Cambio del servidor");
  });
});
