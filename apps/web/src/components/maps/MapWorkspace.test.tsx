import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, it, expect, vi } from "vitest";
import { MapWorkspace } from "./MapWorkspace";
import { json, stubApi } from "@/test-utils";
vi.mock("./MapShell", () => ({ MapShell: () => <div>Geographic canvas</div> }));
vi.mock("./FloorMap", () => ({ FloorMap: ({ floor, onDirty }: {
    floor: {
      name: string;
    };
    onDirty: (dirty: boolean) => void;
  }) => <div>{floor.name}<button onClick={() => onDirty(true)}>Stage floor</button></div> }));
afterEach(() => vi.unstubAllGlobals());
const floor = { id: "f", building_id: "b", name: "Plant plan", ordinal: 0, revision: 1 };
async function setup(extra: Record<string, () => Response> = {}, props: Record<string, unknown> = {}) {
  vi.stubGlobal("fetch", vi.fn(stubApi({
    "/api/v1/me": () => json({ id: "u", grants: [{ permission: "maps.view", effect: "allow", scope_type: "platform" }] }),
    "/api/v1/maps/overview": () => json({ items: [{ id: "s", name: "City", online_cameras: 1, offline_cameras: 0, degraded_cameras: 0, alarm_count: 0 }] }),
    "/api/v1/maps/sites/s": () => json({ id: "s", name: "City", buildings: [{ id: "b", site_id: "s", name: "Plant", floors: [floor] }] }), ...extra,
  })));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><MapWorkspace initialSiteId="s" {...props}/></QueryClientProvider>);
  await screen.findByRole("option", { name: "Plant / Plant plan" });
}
it("keeps geography default and validates a floor against its active site", async () => {
  await setup({}, { initialFloorId: "not-in-site" });
  expect(screen.queryByText("Geographic canvas")).not.toBeInTheDocument();
  expect(screen.getByRole("alert")).toHaveTextContent("no está en este sitio");
  fireEvent.click(screen.getByRole("button", { name: "Abrir mapa geográfico" }));
  await screen.findByText("Geographic canvas");
});
it("selects one floor without constructing geography or offering unauthorized writes", async () => {
  await setup({}, { initialFloorId: "f" });
  expect(screen.queryByText("Geographic canvas")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Create map" })).not.toBeInTheDocument();
  expect(screen.getByRole("combobox", { name: "Mapa" })).toHaveValue("f");
});
it("requires explicit discard before changing maps with pending floor edits", async () => {
  const select = vi.fn();
  await setup({}, { initialFloorId: "f", onSelectFloor: select });
  fireEvent.click(screen.getByRole("button", { name: "Stage floor" }));
  fireEvent.change(screen.getByRole("combobox", { name: "Mapa" }), { target: { value: "" } });
  expect(select).not.toHaveBeenCalled();
  expect(screen.getByRole("combobox", { name: "Mapa" })).toHaveValue("f");
  fireEvent.click(screen.getByRole("button", { name: "Descartar y cambiar" }));
  await waitFor(() => expect(select).toHaveBeenCalledWith(undefined));
});
