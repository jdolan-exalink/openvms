import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json } from "@/test-utils";
import { MoreActions, RESTART_CAMERA_LABEL, RESTART_CAMERA_WARNING, RestartCameraDialog } from "./MoreActions";

afterEach(() => vi.unstubAllGlobals());

const wrap = (ui: React.ReactNode) => render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}>{ui}</QueryClientProvider>);

describe("MoreActions", () => {
  it("lists the actions and runs the chosen one", () => {
    const onSelect = vi.fn();
    wrap(<MoreActions actions={[{ id: "restart", label: RESTART_CAMERA_LABEL, onSelect }]} />);
    fireEvent.click(screen.getByRole("button", { name: "Más acciones" }));
    fireEvent.click(screen.getByRole("menuitem", { name: RESTART_CAMERA_LABEL }));
    expect(onSelect).toHaveBeenCalled();
  });
});

describe("RestartCameraDialog", () => {
  it("warns, sends confirm:true only after the user confirms, and reports the result", async () => {
    const sent: Array<{ path: string; body: unknown }> = [];
    vi.stubGlobal("fetch", async (req: Request) => {
      sent.push({ path: new URL(req.url).pathname, body: await req.json() });
      return json({ restarted: true, via: "agent" });
    });
    const onClose = vi.fn();
    wrap(<RestartCameraDialog cameraId="c1" onClose={onClose} />);
    expect(screen.getByText(RESTART_CAMERA_WARNING)).toBeInTheDocument();
    expect(sent).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Reiniciar" }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toEqual({ path: "/api/v1/cameras/c1/restart", body: { confirm: true } });
    expect(await screen.findByText(/aceptó el reinicio/)).toBeInTheDocument();
  });

  it("cancelling sends nothing and shows the error when the restart fails", async () => {
    const fetchMock = vi.fn(async () => json({ code: "restart_failed", message: "The camera did not accept the restart" }, 502));
    vi.stubGlobal("fetch", fetchMock);
    const onClose = vi.fn();
    wrap(<RestartCameraDialog cameraId="c1" onClose={onClose} />);
    fireEvent.click(screen.getByRole("button", { name: "Reiniciar" }));
    expect(await screen.findByText(/did not accept the restart/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Cancel|Cancelar/ }));
    expect(onClose).toHaveBeenCalled();
  });
});
