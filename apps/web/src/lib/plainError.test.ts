import { describe, expect, it } from "vitest";
import { plainErrorOf } from "./plainError";

describe("plainErrorOf", () => {
  it("prefers the API plain error and keeps the technical text apart", () => {
    const e = plainErrorOf({ plain_error: { message: "No se pudo iniciar el stream de grabación.", technical: "ffmpeg exited with code 1" }, state: { last_error: "raw" } });
    expect(e).toEqual({ message: "No se pudo iniciar el stream de grabación.", technical: "ffmpeg exited with code 1" });
  });

  it("never turns the raw last_error into the message", () => {
    expect(plainErrorOf({ state: { last_error: "ffmpeg exited with code 1" } })).toEqual({ message: "No se pudo aplicar la configuración.", technical: "ffmpeg exited with code 1" });
  });

  it("is null without a failure", () => {
    expect(plainErrorOf(undefined)).toBeNull();
    expect(plainErrorOf({ state: { last_error: "" } })).toBeNull();
  });
});
