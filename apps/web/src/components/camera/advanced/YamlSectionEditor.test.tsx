import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { YamlSectionEditor } from "./YamlSectionEditor";

afterEach(() => vi.unstubAllGlobals());

const base = { section: "detect", valid: true, schema_checked: true, syntax: [], schema: [], changes: [], managed_conflicts: [], warnings: [], revision: 7 };
const source = { camera_id: "c1", section: "detect", yaml: "enabled: true\nfps: 5\n", exists: true, secrets_visible: true, managed_paths: [], revision: 7 };
const state = { desired_revision: 7, applied_revision: 7, sync_state: "synced", last_error: "", drift: [] };

function mount(routes: Record<string, () => Response>, props: Partial<React.ComponentProps<typeof YamlSectionEditor>> = {}) {
  const calls: Array<{ url: string; body: unknown }> = [];
  const h = stubApi({
    "/api/v1/cameras/c1/frigate-yaml": () => json(source),
    "/api/v1/cameras/c1/normalized-config": () => json({ state }),
    ...routes,
  });
  vi.stubGlobal("fetch", async (req: Request) => {
    calls.push({ url: new URL(req.url).pathname, body: req.method === "POST" ? await req.clone().json() : undefined });
    return h(req);
  });
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <YamlSectionEditor cameraId="c1" section="detect" canEdit {...props} />
    </QueryClientProvider>,
  );
  return calls;
}

const edit = async (value: string) => {
  const box = await screen.findByLabelText("YAML de detect");
  fireEvent.change(box, { target: { value } });
};
const save = () => screen.getByRole("button", { name: "Guardar" });

describe("YamlSectionEditor", () => {
  it("loads the section and keeps Guardar disabled until a valid, changed YAML was validated", async () => {
    mount({ "/api/v1/cameras/c1/frigate-yaml/validate": () => json({ ...base, changes: [{ path: "fps", kind: "changed", from: 5, to: 10 }] }) });
    expect(await screen.findByDisplayValue(/fps: 5/)).toBeInTheDocument();
    expect(save()).toBeDisabled();
    await edit("enabled: true\nfps: 10\n");
    expect(save()).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Validar" }));
    expect(await screen.findByText(/Cambiado fps: 5 → 10/)).toBeInTheDocument();
    expect(save()).toBeEnabled();
    await edit("enabled: true\nfps: 11\n"); // editing again invalidates the check
    expect(save()).toBeDisabled();
  });

  it("lists syntax errors with line and column and blocks Guardar", async () => {
    mount({ "/api/v1/cameras/c1/frigate-yaml/validate": () => json({ ...base, valid: false, syntax: [{ line: 3, column: 5, message: "mapping values are not allowed here" }], schema: [{ path: "fps", message: "debe ser un entero" }] }) });
    await edit("enabled: [");
    fireEvent.click(screen.getByRole("button", { name: "Validar" }));
    expect(await screen.findByText("Línea 3, columna 5: mapping values are not allowed here")).toBeInTheDocument();
    expect(screen.getByText("fps: debe ser un entero")).toBeInTheDocument();
    expect(save()).toBeDisabled();
  });

  it("requires an explicit confirmation of managed parameters before saving, and sends it", async () => {
    const conflict = { patch_path: "detection.enabled", engine_path: "enabled", desired: true, new: false };
    const calls = mount({
      "/api/v1/cameras/c1/frigate-yaml/validate": () => json({ ...base, changes: [{ path: "enabled", kind: "changed", from: true, to: false }], managed_conflicts: [conflict] }),
      "/api/v1/cameras/c1/frigate-yaml/apply": () => json({ validation: base, frigate: { revision_id: "r", sections: [], restart_required: false }, marked_drift: true, revision: 8, sync_state: "drift" }),
    });
    await edit("enabled: false\nfps: 5\n");
    fireEvent.click(screen.getByRole("button", { name: "Validar" }));
    expect(await screen.findByText(/Estos parámetros los administra OpenVMS: detection.enabled/)).toBeInTheDocument();
    expect(save()).toBeDisabled();
    fireEvent.click(screen.getByRole("checkbox"));
    expect(save()).toBeEnabled();
    fireEvent.click(save());
    expect(await screen.findByText(/la cámara quedó con diferencia detectada/)).toBeInTheDocument();
    expect(calls.find((c) => c.url.endsWith("/apply"))?.body).toEqual({ section: "detect", yaml: "enabled: false\nfps: 5\n", expected_revision: 7, confirm_managed_conflicts: true });
  });

  it("revalidates to show the conflicts when the server answers 409 managed_conflict", async () => {
    let validations = 0;
    mount({
      "/api/v1/cameras/c1/frigate-yaml/validate": () => {
        validations++;
        return json(validations === 1
          ? { ...base, changes: [{ path: "fps", kind: "changed", from: 5, to: 6 }] }
          : { ...base, changes: [{ path: "fps", kind: "changed", from: 5, to: 6 }], managed_conflicts: [{ patch_path: "detection.fps", engine_path: "fps", desired: 5, new: 6 }] });
      },
      "/api/v1/cameras/c1/frigate-yaml/apply": () => json({ code: "managed_conflict", message: "managed" }, 409),
    });
    await edit("enabled: true\nfps: 6\n");
    fireEvent.click(screen.getByRole("button", { name: "Validar" }));
    await screen.findByText(/Cambiado fps/);
    fireEvent.click(save());
    expect(await screen.findByText(/Estos parámetros los administra OpenVMS: detection.fps/)).toBeInTheDocument();
    expect(save()).toBeDisabled();
  });

  it("offers to reload on 409 revision_conflict instead of overwriting", async () => {
    mount({
      "/api/v1/cameras/c1/frigate-yaml/validate": () => json({ ...base, changes: [{ path: "fps", kind: "changed", from: 5, to: 6 }] }),
      "/api/v1/cameras/c1/frigate-yaml/apply": () => json({ code: "revision_conflict", message: "stale" }, 409),
    });
    await edit("enabled: true\nfps: 6\n");
    fireEvent.click(screen.getByRole("button", { name: "Validar" }));
    await screen.findByText(/Cambiado fps/);
    fireEvent.click(save());
    expect(await screen.findByText(/La configuración cambió mientras editabas/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Recargar YAML" })).toBeInTheDocument();
  });

  it("shows the reason instead of the editor without cameras.config.yaml", async () => {
    const calls = mount({}, { canEdit: false });
    expect(await screen.findByText(/cameras.config.yaml/)).toBeInTheDocument();
    expect(screen.queryByLabelText("YAML de detect")).toBeNull();
    await waitFor(() => expect(calls.filter((c) => c.url.endsWith("/frigate-yaml"))).toHaveLength(0));
  });
});
