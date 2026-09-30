import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { FrigateCameraConfig } from "./FrigateCameraConfig";

afterEach(() => vi.unstubAllGlobals());

const schema = {
  $ref: "#/$defs/CameraConfig",
  $defs: {
    CameraConfig: {
      type: "object",
      properties: { detect: { $ref: "#/$defs/Detect" }, onvif: { $ref: "#/$defs/Onvif" } },
    },
    Detect: { type: "object", properties: { enabled: { type: "boolean", default: true }, fps: { type: "integer", minimum: 1, maximum: 30, description: "Frames por segundo" } } },
    Onvif: { type: "object", properties: { host: { type: "string" }, password: { type: "string" } } },
  },
};

function mount(doc: object, extra: Record<string, () => Response> = {}) {
  const routes = stubApi({
    "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [{ permission: "servers.restart", effect: "allow" }] }),
    "/api/v1/cameras/c1/frigate-config": () => json({ camera_id: "c1", camera_name: "acceso", server_id: "s1", frigate_version: "0.17.0", editable: true, secrets_visible: false, config: { detect: { enabled: true, fps: 5 }, onvif: { host: "10.0.0.2", password: "********" } }, ...doc }),
    "/api/v1/servers/s1/frigate-config/schema": () => json(schema),
    ...extra,
  });
  const fetchMock = vi.fn(async (req: Request) => routes(req));
  vi.stubGlobal("fetch", fetchMock);
  const root = createRootRoute();
  const page = createRoute({ getParentRoute: () => root, path: "/cameras/$cameraId/frigate", component: FrigateCameraConfig });
  const cams = createRoute({ getParentRoute: () => root, path: "/cameras", component: () => null });
  const router = createRouter({ routeTree: root.addChildren([page, cams]), history: createMemoryHistory({ initialEntries: ["/cameras/c1/frigate"] }) });
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return fetchMock;
}

describe("FrigateCameraConfig", () => {
  it("edits a section, shows the diff and patches only the changed keys", async () => {
    const fetchMock = mount({}, {
      "/api/v1/cameras/c1/frigate-config": () => json({ camera_id: "c1", camera_name: "acceso", server_id: "s1", frigate_version: "0.16.1", editable: true, secrets_visible: false, config: { detect: { enabled: true, fps: 5 }, onvif: { host: "h", password: "****" } } }),
    });
    const nav = await screen.findByRole("navigation", { name: "Secciones de configuración" });
    expect(within(nav).getByText("Detección")).toBeInTheDocument();
    expect(within(nav).getAllByText("Requiere reinicio").length).toBeGreaterThan(0);

    fireEvent.change(await screen.findByLabelText("Fps"), { target: { value: "12" } });
    fireEvent.click(screen.getByRole("button", { name: "Revisar cambios" }));
    const dialog = await screen.findByRole("dialog", { name: "Revisar cambios" });
    expect(within(dialog).getByText("12")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Aplicar" }));

    await waitFor(() => {
      const patch = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "PATCH");
      expect(patch).toBeDefined();
    });
    const patch = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "PATCH")!;
    expect(await patch.clone().json()).toEqual({ sections: { detect: { fps: 12 } } });
  });

  it("blocks review on invalid values and locks credentials without the secrets permission", async () => {
    mount({});
    fireEvent.change(await screen.findByLabelText("Fps"), { target: { value: "99" } });
    expect(await screen.findByText("Debe ser como máximo 30.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revisar cambios" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: /ONVIF/ }));
    expect(await screen.findByLabelText("Host")).toHaveValue("10.0.0.2");
    expect(screen.getAllByText("Protegido: requiere permiso de credenciales").length).toBe(1);
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
  });

  it("is read-only on Frigate older than 0.16", async () => {
    mount({ frigate_version: "0.14.1", editable: false });
    expect(await screen.findByText(/requiere 0\.16 o superior/)).toBeInTheDocument();
    expect(await screen.findByLabelText("Fps")).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Revisar cambios" })).not.toBeInTheDocument();
  });
});
