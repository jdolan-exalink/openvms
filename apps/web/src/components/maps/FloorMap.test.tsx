import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, it, expect, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { FloorMap } from "./FloorMap";
const h = vi.hoisted(() => ({ navigate: vi.fn() }));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => h.navigate }));
vi.mock("./canvas/FloorPlanCanvas", () => ({ FloorPlanCanvas: ({ cameras, onPlace }: {
    cameras: {
      id: string;
      name: string;
    }[];
    onPlace: (id: string, p: {
      x: number;
      y: number;
    }) => void;
  }) => <div>{cameras.map(c => <span key={c.id}>{c.name}</span>)}<button onClick={() => onPlace("new", { x: .3, y: .6 })}>Drop camera</button><button onClick={() => onPlace("foreign", { x: .3, y: .6 })}>Drop foreign</button></div> }));
vi.mock("./editor/PlanUpload", () => ({ PlanUpload: () => <div>Upload controls</div> }));
afterEach(() => { vi.unstubAllGlobals(); h.navigate.mockReset(); });
async function setup(permissions: string[], conflict = false, slowRefresh = false, staleUnplaced = false) {
  const writes: Request[] = [];
  let reads = 0;
  let release: () => void = () => { };
  const handler = stubApi({
    "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: permissions.map(permission => ({ permission, effect: "allow", scope_type: "platform" })) }),
    "/api/v1/maps/sites/s/entities": () => json({ revision: 1, entities: [{ id: "geo", t: "camera", site: "s", name: "Geographic only", st: "online", pos: { k: "geo", lat: 1, lng: 2 }, rev: 4, alarms: 0, cam: {} }, { id: "old", t: "camera", site: "s", name: "Floor camera", st: "offline", pos: { k: "floor", floor_id: "f", x: .1, y: .2 }, rev: 7, alarms: 2, cam: { type: "fixed", fov: 60, range: 100 } }, { id: "foreign", t: "camera", site: "s", name: "Other floor", st: "online", pos: { k: "floor", floor_id: "other", x: .1, y: .2 }, rev: 1, cam: {} }] }),
    "/api/v1/maps/unplaced": () => json({ cameras: [{ id: "new", name: "New camera", site_id: "s", status: "unknown" }, ...(staleUnplaced ? [{ id: "old", name: "Floor camera", site_id: "s", status: "offline" }] : [])] }),
    "/api/v1/maps/placements/camera/new": () => json(conflict ? { message: "Placement changed" } : { revision: 1 }, conflict ? 409 : 200),
  });
  const fetch = vi.fn((request: Request) => {
    if (new URL(request.url).pathname === "/api/v1/maps/sites/s/entities" && ++reads > 1 && slowRefresh)
      return new Promise<Response>(resolve => { release = () => { void handler(request).then(resolve); }; });
    if (new URL(request.url).pathname === "/api/v1/maps/placements/camera/new")
      writes.push(request.clone());
    return handler(request);
  });
  vi.stubGlobal("fetch", fetch);
  const dirty = vi.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><FloorMap siteId="s" floor={{ id: "f", building_id: "b", name: "Plan", ordinal: 0, revision: 1 }} initialMode="edit" onDirty={dirty} onPlanSaved={vi.fn()}/></QueryClientProvider>);
  await screen.findByRole("button", { name: "Floor camera" });
  return { writes, dirty, fetch, release: () => release() };
}
it("isolates floor positions and stages direct drag until explicit save", async () => {
  const { writes, dirty } = await setup(["maps.edit_device"]);
  expect(screen.queryByText("Geographic only")).not.toBeInTheDocument();
  expect(screen.queryByText("Other floor")).not.toBeInTheDocument();
  fireEvent.click(screen.getByText("Drop foreign"));
  expect(screen.queryByText("1 cambio(s) sin guardar")).not.toBeInTheDocument();
  fireEvent.click(screen.getByText("Drop camera"));
  expect(writes).toHaveLength(0);
  expect(dirty).toHaveBeenLastCalledWith(true);
  fireEvent.click(screen.getByText("Guardar (1)"));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(await writes[0]!.json()).toEqual({ site_id: "s", floor_id: "f", x: .3, y: .6 });
  await waitFor(() => expect(dirty).toHaveBeenLastCalledWith(false));
});
it("gates uploads and writes independently, preserving failed changes without retry", async () => {
  const { writes } = await setup(["maps.edit_device"], true);
  expect(screen.queryByText("Upload controls")).not.toBeInTheDocument();
  fireEvent.click(screen.getByText("Drop camera"));
  fireEvent.click(screen.getByText("Guardar (1)"));
  await screen.findByText(/new: Placement changed/);
  expect(writes).toHaveLength(1);
  expect(screen.getByText("1 cambio(s) sin guardar")).toBeInTheDocument();
  fireEvent.click(screen.getByText("Cancelar"));
  expect(screen.queryByText("1 cambio(s) sin guardar")).not.toBeInTheDocument();
});
it("maps.edit alone can upload but cannot place cameras", async () => {
  const { writes } = await setup(["maps.edit"]);
  expect(screen.getByText("Upload controls")).toBeInTheDocument();
  fireEvent.click(screen.getByText("Drop camera"));
  expect(screen.queryByText("1 cambio(s) sin guardar")).not.toBeInTheDocument();
  expect(writes).toHaveLength(0);
});
it("keeps pending markers and navigation dirty until post-save refresh finishes", async () => {
  const { writes, dirty, release } = await setup(["maps.edit_device"], false, true);
  fireEvent.click(screen.getByText("Drop camera"));
  await screen.findByRole("button", { name: "New camera" });
  fireEvent.click(screen.getByText("Guardar (1)"));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(screen.getAllByText("New camera").length).toBeGreaterThan(1);
  expect(dirty).toHaveBeenLastCalledWith(true);
  release();
  await waitFor(() => expect(dirty).toHaveBeenLastCalledWith(false));
});
it("does not duplicate a placed camera when unplaced cache is temporarily stale", async () => {
  await setup(["maps.edit_device"], false, false, true);
  expect(screen.getAllByRole("button", { name: "Floor camera" })).toHaveLength(1);
});
