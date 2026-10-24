import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, postForm, requestJson } from "./client";

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

describe("requestJson", () => {
  const lastInit = (mock: ReturnType<typeof vi.fn>) => (mock.mock.calls[0] as unknown as [string, RequestInit])[1];

  it("sends the CSRF header and a JSON body, and returns the parsed answer", async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ ok: true }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(requestJson("POST", "/api/v1/x", { a: 1 })).resolves.toEqual({ ok: true });
    const init = lastInit(fetchMock);
    const headers = new Headers(init.headers);
    expect(init.method).toBe("POST");
    expect(headers.get("X-OpenVMS-Request")).toBe("1");
    expect(headers.get("Content-Type")).toBe("application/json");
    expect(init.body).toBe('{"a":1}');
  });

  it("sends no body or content type when there is none", async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ items: [] }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await requestJson("GET", "/api/v1/x");
    const init = lastInit(fetchMock);
    expect(init.body).toBeUndefined();
    expect(new Headers(init.headers).has("Content-Type")).toBe(false);
  });

  it("returns undefined for an empty 204 answer", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 204 })));
    await expect(requestJson("DELETE", "/api/v1/x")).resolves.toBeUndefined();
  });

  it("throws ApiError with the contract's code and message", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ code: "registry_rejected", message: "address in use" }), { status: 409 })));
    await expect(requestJson("PUT", "/api/v1/x", {})).rejects.toMatchObject({ status: 409, code: "registry_rejected", message: "address in use" });
  });

  it("falls back to a generic message when the error body is not JSON", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("<html>bad gateway</html>", { status: 502 })));
    const err = await requestJson("GET", "/api/v1/x").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 502, code: "error" });
  });
});
