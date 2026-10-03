import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { AlarmPanel } from "./AlarmPanel";

afterEach(() => vi.unstubAllGlobals());
function setup(manage = true, fail = false) {
  const writes: string[] = [];
  vi.stubGlobal("fetch", vi.fn(stubApi({
    "/api/v1/alarms/a/transitions": () => json([{ id: 1, comment: "Existing note", at: "2026-10-01", to_status: "open" }]),
    "/api/v1/alarms/a/assignees": () => json({ items: [{ id: "u", username: "Operator" }] }),
    ...Object.fromEntries(["acknowledge", "assign", "investigate", "resolve", "close", "comments"].map(action => [
      `/api/v1/alarms/a/${action}`, () => { writes.push(action); return fail ? json({ code: "forbidden", message: "Denied" }, 403) : json({ id: "a" }); },
    ])),
  })));
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <AlarmPanel alarms={[{ id: "a", status: "open", camera_name: "Entrance" } as never]} canManage={manage} />
  </QueryClientProvider>);
  return writes;
}
it("offers read-only history without management controls", async () => {
  setup(false);
  fireEvent.click(screen.getByRole("button", { name: /Entrance/ }));
  expect(await screen.findByText(/Existing note/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Marcar como vista" })).not.toBeInTheDocument();
});
it("uses all authorized lifecycle endpoints and comments", async () => {
  const writes = setup();
  fireEvent.click(screen.getByRole("button", { name: /Entrance/ }));
  await screen.findByText(/Existing note/);
  fireEvent.change(screen.getByLabelText("Comment"), { target: { value: "Investigating" } });
  fireEvent.click(screen.getByRole("button", { name: "Marcar como vista" }));
  await screen.findByText("Acción registrada");
  for (const action of ["Investigar", "Resolver", "Cerrar alarma", "Guardar comentario"]) {
    fireEvent.click(screen.getByRole("button", { name: action }));
    await screen.findByText("Acción registrada");
  }
  fireEvent.change(screen.getByLabelText("Assignee"), { target: { value: "u" } });
  fireEvent.click(screen.getByRole("button", { name: "Asignar" }));
  await screen.findByText("Acción registrada");
  expect(writes).toEqual(["acknowledge", "comments", "investigate", "resolve", "close", "comments", "assign"]);
});
it("shows API authorization errors rather than claiming success", async () => {
  setup(true, true);
  fireEvent.click(screen.getByRole("button", { name: /Entrance/ }));
  fireEvent.click(await screen.findByRole("button", { name: "Resolver" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Denied");
});
