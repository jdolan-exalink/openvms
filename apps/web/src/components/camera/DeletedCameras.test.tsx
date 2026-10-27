import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json } from "@/test-utils";
import { DeletedCameras } from "./DeletedCameras";
import { DeleteCameraDialog } from "./MoreActions";

afterEach(() => vi.unstubAllGlobals());

type Sent = { method: string; path: string };

const wrap = (ui: React.ReactNode) =>
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{ui}</QueryClientProvider>);

const deletedCamera = (over: object = {}) => ({
  id: "c1", server_id: "s1", remote_name: "plaza", display_name: "Plaza", deleted_at: "2026-10-27T20:00:00Z", retain_days: 7, has_frigate_config: true, ...over,
});

function stub(routes: Record<string, () => Response>) {
  const sent: Sent[] = [];
  vi.stubGlobal("fetch", async (req: Request) => {
    const path = new URL(req.url).pathname;
    sent.push({ method: req.method, path });
    const handler = routes[`${req.method} ${path}`];
    return handler ? handler() : json({ code: "not_found", message: "x" }, 404);
  });
  return sent;
}

describe("DeleteCameraDialog", () => {
  it("warns before anything is sent and deletes only on confirm", async () => {
    const sent = stub({ "DELETE /api/v1/cameras/c1": () => new Response(null, { status: 204 }) });
    const onDeleted = vi.fn();
    wrap(<DeleteCameraDialog cameraId="c1" name="Plaza" onClose={() => {}} onDeleted={onDeleted} />);

    const message = screen.getByText(/Se eliminará "Plaza" de OpenVMS y de la configuración de Frigate/);
    expect(message).toHaveTextContent("dejará de grabar");
    expect(message).toHaveTextContent("Frigate se reiniciará");
    expect(message).toHaveTextContent("se conservan hasta que pasen sus días de grabación");
    expect(sent).toEqual([]);

    fireEvent.click(screen.getByRole("button", { name: "Eliminar" }));
    await waitFor(() => expect(onDeleted).toHaveBeenCalledOnce());
    expect(sent).toEqual([{ method: "DELETE", path: "/api/v1/cameras/c1" }]);
  });

  it("sends nothing when cancelled", () => {
    const sent = stub({});
    const onClose = vi.fn();
    wrap(<DeleteCameraDialog cameraId="c1" name="Plaza" onClose={onClose} onDeleted={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Cancelar" }));
    expect(onClose).toHaveBeenCalled();
    expect(sent).toEqual([]);
  });

  it("keeps the dialog open and shows the error when Frigate cannot be reached", async () => {
    stub({ "DELETE /api/v1/cameras/c1": () => json({ code: "frigate_unreachable", message: "Frigate no responde" }, 502) });
    const onDeleted = vi.fn();
    wrap(<DeleteCameraDialog cameraId="c1" name="Plaza" onClose={() => {}} onDeleted={onDeleted} />);
    fireEvent.click(screen.getByRole("button", { name: "Eliminar" }));
    expect(await screen.findByText(/Frigate no responde/)).toBeInTheDocument();
    expect(onDeleted).not.toHaveBeenCalled();
  });
});

describe("DeletedCameras", () => {
  const list = (items: object[]) => ({ "GET /api/v1/servers/s1/deleted-cameras": () => json({ items }) });

  it("renders nothing when the server has no deleted cameras", async () => {
    const sent = stub(list([]));
    const { container } = wrap(<DeletedCameras serverId="s1" />);
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(container).toBeEmptyDOMElement();
  });

  it("lists deleted cameras and restores one only after warning that the history is merged", async () => {
    const sent = stub({
      ...list([deletedCamera()]),
      "POST /api/v1/cameras/c1/restore": () => json({ id: "c1" }),
    });
    wrap(<DeletedCameras serverId="s1" />);
    expect(await screen.findByText(/Plaza/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Restaurar" }));
    const dialog = screen.getByRole("dialog", { name: "Restaurar cámara" });
    expect(dialog).toHaveTextContent("el historial anterior se unirá con las grabaciones nuevas, sin pisarlo");
    expect(dialog).toHaveTextContent("Frigate, que se reiniciará");
    expect(sent.filter((s) => s.method === "POST")).toHaveLength(0);

    fireEvent.click(screen.getAllByRole("button", { name: "Restaurar" }).at(-1)!);
    await waitFor(() => expect(sent.filter((s) => s.method === "POST")).toEqual([{ method: "POST", path: "/api/v1/cameras/c1/restore" }]));
  });

  it("says so when the camera's Frigate configuration was not kept", async () => {
    stub(list([deletedCamera({ has_frigate_config: false })]));
    wrap(<DeletedCameras serverId="s1" />);
    await screen.findByText(/Plaza/);
    fireEvent.click(screen.getByRole("button", { name: "Restaurar" }));
    expect(screen.getByRole("dialog", { name: "Restaurar cámara" })).toHaveTextContent("solo se restaura en OpenVMS");
  });

  it("shows the conflict of a live camera with the same name", async () => {
    stub({
      ...list([deletedCamera()]),
      "POST /api/v1/cameras/c1/restore": () => json({ code: "conflict", message: "Ya existe una cámara con ese nombre" }, 409),
    });
    wrap(<DeletedCameras serverId="s1" />);
    await screen.findByText(/Plaza/);
    fireEvent.click(screen.getByRole("button", { name: "Restaurar" }));
    fireEvent.click(screen.getAllByRole("button", { name: "Restaurar" }).at(-1)!);
    expect(await screen.findByText(/Ya existe una cámara con ese nombre/)).toBeInTheDocument();
  });
});
