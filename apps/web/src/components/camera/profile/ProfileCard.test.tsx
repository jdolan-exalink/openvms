import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { CameraChangeProvider } from "@/components/camera/CameraChangeProvider";
import { ProfileCard } from "./ProfileCard";
import { ProvenanceBadge } from "./ProvenanceBadge";

afterEach(() => vi.unstubAllGlobals());

const state = { desired_revision: 1, applied_revision: 1, sync_state: "synced", last_error: "", drift: [] };
const me = (perms: string[]) => json({ id: "u", tenant_id: "t1", grants: perms.map((permission) => ({ permission, effect: "allow", scope_type: "platform" })) });
const review = (extra: object = {}) => ({
  parameters: 2,
  review: { changes: [{ path: "detection.enabled", label: "detección de objetos", from: false, to: true }, { path: "streams.primary.fps", label: "FPS", from: 5, to: 7 }], impact: [], unsupported: [] },
  shadowed: [],
  converted: [],
  dropped: [],
  ...extra,
});

function mount(config: Record<string, unknown>, perms = ["cameras.manage"], routes: Record<string, () => Response> = {}, children?: React.ReactNode) {
  const calls: { path: string; body: unknown }[] = [];
  const base = stubApi({
    "/api/v1/me": () => me(perms),
    "/api/v1/cameras/c1/normalized-config": () => json({ state, ...config }),
    "/api/v1/camera-config/profiles": () => json([{ id: "p1", name: "Exterior", description: "", patch: {}, version: 2, camera_count: 3, created_at: "x", updated_at: "x" }]),
    ...routes,
  });
  vi.stubGlobal("fetch", vi.fn(async (req: Request) => {
    if (req.method === "POST") calls.push({ path: new URL(req.url).pathname, body: await req.clone().json() });
    return base(req);
  }));
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <CameraChangeProvider cameraId="c1">
        <ProfileCard cameraId="c1" />
        {children}
      </CameraChangeProvider>
    </QueryClientProvider>,
  );
  return calls;
}

describe("ProfileCard", () => {
  it("shows the no-profile state and lets the user pick one", async () => {
    mount({});
    expect(await screen.findByText(/Sin perfil/)).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Cambiar perfil" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Desvincular perfil" })).toBeNull();
  });

  it("shows the applied profile with name and version", async () => {
    mount({ profile: { id: "p1", name: "Exterior", version: 2, outdated: false } });
    expect(await screen.findByText("Exterior")).toBeInTheDocument();
    expect(screen.getByText("versión 2")).toBeInTheDocument();
    expect(screen.queryByText("Desactualizado")).toBeNull();
  });

  it("flags an outdated profile", async () => {
    mount({ profile: { id: "p1", name: "Exterior", version: 1, outdated: true } });
    expect(await screen.findByText("Desactualizado")).toBeInTheDocument();
  });

  it("reviews a profile change with the count, the changes and the shadowed notice, then applies", async () => {
    const calls = mount({}, ["cameras.manage"], {
      "/api/v1/cameras/c1/profile/review": () => json(review({ shadowed: ["streams.primary.fps"] })),
      "/api/v1/cameras/c1/profile/apply": () => json({ review: review() }),
    });
    fireEvent.click(await screen.findByRole("button", { name: "Cambiar perfil" }));
    const select = await screen.findByLabelText("Perfil");
    await screen.findByRole("option", { name: "Exterior" });
    fireEvent.change(select, { target: { value: "p1" } });
    expect(await screen.findByText("Se modificarán 2 parámetros.")).toBeInTheDocument();
    expect(screen.getByText(/detección de objetos: OFF → ON/)).toBeInTheDocument();
    expect(screen.getByText(/FPS: 5 → 7/)).toBeInTheDocument();
    expect(screen.getByRole("note")).toHaveTextContent("streams.primary.fps");
    fireEvent.click(screen.getByRole("button", { name: "Aplicar" }));
    expect(await screen.findByText("Perfil aplicado.")).toBeInTheDocument();
    expect(calls.filter((c) => c.path.endsWith("/profile/apply"))[0]?.body).toEqual({ profile_id: "p1", keep_values: true });
  });

  it("cancel closes the dialog without applying", async () => {
    const calls = mount({}, ["cameras.manage"], { "/api/v1/cameras/c1/profile/review": () => json(review()) });
    fireEvent.click(await screen.findByRole("button", { name: "Cambiar perfil" }));
    fireEvent.click(await screen.findByRole("button", { name: "Cancelar" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(calls.some((c) => c.path.endsWith("/profile/apply"))).toBe(false);
  });

  it.each([
    ["Conservar los valores actuales como cambios locales", true],
    ["Descartar los valores del perfil", false],
  ])("unlinks with keep_values for %s", async (label, keep) => {
    const calls = mount({ profile: { id: "p1", name: "Exterior", version: 2, outdated: false } }, ["cameras.manage"], {
      "/api/v1/cameras/c1/profile/review": () => json(review({ converted: ["a", "b"], dropped: ["a"] })),
      "/api/v1/cameras/c1/profile/apply": () => json({ review: review() }),
    });
    fireEvent.click(await screen.findByRole("button", { name: "Desvincular perfil" }));
    fireEvent.click(await screen.findByLabelText(label));
    await waitFor(() => expect(screen.getByTestId("unlink-summary")).toHaveTextContent(keep ? "2 valores como cambios locales" : "1 valores del perfil"));
    fireEvent.click(screen.getByRole("button", { name: "Desvincular" }));
    expect(await screen.findByText("Perfil desvinculado.")).toBeInTheDocument();
    expect(calls.filter((c) => c.path.endsWith("/profile/apply"))[0]?.body).toEqual({ profile_id: null, keep_values: keep });
  });

  it("lists the local overrides", async () => {
    mount({ provenance: { "detection.enabled": { source: "override" }, "recording.enabled": { source: "profile", profile_name: "Exterior" } } });
    fireEvent.click(await screen.findByRole("button", { name: "Ver overrides" }));
    const list = screen.getByRole("list", { name: "Overrides" });
    expect(list).toHaveTextContent("detection.enabled");
    expect(list).not.toHaveTextContent("recording.enabled");
  });

  it("is read-only without cameras.manage", async () => {
    mount({ profile: { id: "p1", name: "Exterior", version: 2, outdated: false } }, ["cameras.view"]);
    await screen.findByText("Exterior");
    expect(screen.queryByRole("button", { name: "Cambiar perfil" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Desvincular perfil" })).toBeNull();
    expect(screen.getByRole("button", { name: "Ver overrides" })).toBeInTheDocument();
  });
});

describe("ProvenanceBadge", () => {
  it("labels the three sources and names the profile in the tooltip", async () => {
    mount(
      { provenance: { "detection.enabled": { source: "engine" }, "recording.enabled": { source: "profile", profile_name: "Exterior" }, "analytics.lpr": { source: "override" } } },
      ["cameras.view"],
      {},
      <>
        <ProvenanceBadge path="detection.enabled" />
        <ProvenanceBadge path="recording.enabled" />
        <ProvenanceBadge path="analytics.lpr" />
        <ProvenanceBadge path="unknown.path" />
      </>,
    );
    expect(await screen.findByText("Heredado")).toBeInTheDocument();
    expect(screen.getByText("Definido por perfil").closest("[title]")).toHaveAttribute("title", "Perfil: Exterior");
    expect(screen.getByText("Modificado localmente")).toBeInTheDocument();
    expect(screen.getAllByTestId("provenance-badge")).toHaveLength(3);
  });
});
