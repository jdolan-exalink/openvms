import { afterEach, describe, expect, it, vi } from "vitest";
import { json } from "@/test-utils";
import { assignCluster, dismissCluster, PartialAssignError } from "./faceClusters";

afterEach(() => vi.unstubAllGlobals());

function stub(failAt = -1) {
  const calls: { method: string; path: string; body?: unknown }[] = [];
  let n = 0;
  vi.stubGlobal("fetch", vi.fn(async (raw: Request) => {
    const path = new URL(raw.url).pathname;
    const text = await raw.clone().text();
    calls.push({ method: raw.method, path, body: text ? JSON.parse(text) : undefined });
    if (raw.method === "DELETE") return new Response(null, { status: 204 });
    if (n++ === failAt) return json({ code: "internal", message: "boom" }, 500);
    return json({ person: { id: "p-new", display_name: "Julián" }, image: {}, attempt_removed: true }, 201);
  }));
  return calls;
}

describe("assignCluster", () => {
  it("creates the person with the first sighting and reuses it for the rest", async () => {
    const calls = stub();
    const res = await assignCluster(["a", "b", "c"], { newName: "  Julián " });
    expect(calls.map((c) => c.body)).toEqual([{ new_person_display_name: "Julián" }, { person_id: "p-new" }, { person_id: "p-new" }]);
    expect(res).toEqual({ personId: "p-new", name: "Julián", count: 3 });
  });

  it("assigns every sighting to an existing person without creating anyone", async () => {
    const calls = stub();
    await assignCluster(["a", "b"], { personId: "p1" });
    expect(calls.map((c) => c.body)).toEqual([{ person_id: "p1" }, { person_id: "p-new" }]);
  });

  it("reports how many were assigned when one fails and keeps the created person for a retry", async () => {
    stub(1);
    const err = await assignCluster(["a", "b", "c"], { newName: "Julián" }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(PartialAssignError);
    expect(err).toMatchObject({ done: 1, total: 3, personId: "p-new" });
  });
});

describe("dismissCluster", () => {
  it("deletes every sighting", async () => {
    const calls = stub();
    await dismissCluster(["a", "b"]);
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual(["DELETE /api/v1/faces/review-inbox/a", "DELETE /api/v1/faces/review-inbox/b"]);
  });
});
