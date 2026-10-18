import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, postForm } from "./client";

afterEach(() => vi.unstubAllGlobals());

const form = () => {
  const f = new FormData();
  f.append("file", new File([new Uint8Array([1])], "a.png"));
  return f;
};

describe("postForm", () => {
  it("sends the CSRF header and returns the parsed body", async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ id: "i1" }), { status: 201, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(postForm("/api/v1/x", form())).resolves.toEqual({ id: "i1" });
    const init = (fetchMock.mock.calls[0] as unknown as [string, RequestInit])[1];
    expect(new Headers(init.headers).get("X-OpenVMS-Request")).toBe("1");
  });

  it("throws ApiError with the contract's code and message", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ code: "invalid", message: "bad picture" }), { status: 400 })));
    await expect(postForm("/api/v1/x", form())).rejects.toMatchObject({ status: 400, code: "invalid", message: "bad picture" });
  });

  it("falls back to a generic message when the error body is not JSON", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("<html>bad gateway</html>", { status: 502 })));
    const err = await postForm("/api/v1/x", form()).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 502, code: "error" });
    expect((err as ApiError).message).toContain("502");
  });
});
