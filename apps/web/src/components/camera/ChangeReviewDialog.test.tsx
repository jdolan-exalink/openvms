import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { CameraChangeProvider, useCameraChange } from "./CameraChangeProvider";

afterEach(() => vi.unstubAllGlobals());

const state = (over: Record<string, unknown> = {}) => ({ desired_revision: 4, applied_revision: 4, sync_state: "synced", last_error: "", drift: [], ...over });
const model = () => json({ state: state() });
const review = { changes: [{ path: "detection.enabled", label: "detección de vehículos", from: false, to: true }], impact: [{ code: "engine_reload", message: "Frigate recargará configuración;" }], unsupported: [] as string[] };

function Consumer() {
  const c = useCameraChange();
  return (
    <>
      <button type="button" onClick={() => c.setPatch({ detection: { enabled: true } })}>edit</button>
      <button type="button" onClick={c.openReview}>open</button>
      <span data-testid="count">{c.pendingCount}</span>
    </>
  );
}

function mount(routes: Record<string, () => Response>) {
  const calls: Array<{ url: string; body: unknown }> = [];
  const h = stubApi({ "/api/v1/cameras/c1/normalized-config": model, ...routes });
  vi.stubGlobal("fetch", async (req: Request) => {
    calls.push({ url: new URL(req.url).pathname, body: req.method === "POST" ? await req.clone().json() : undefined });
    return h(req);
  });
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <CameraChangeProvider cameraId="c1"><Consumer /></CameraChangeProvider>
    </QueryClientProvider>,
  );
  return calls;
}

const openDialog = async () => {
  fireEvent.click(screen.getByText("edit"));
  fireEvent.click(screen.getByText("open"));
  await screen.findByRole("dialog", { name: "Revisar cambios" });
};

describe("ChangeReviewDialog", () => {
  it("shows Cambios and Impacto and sends expected_revision on Aplicar cambios", async () => {
    const calls = mount({
      "/api/v1/cameras/c1/normalized-config/review": () => json(review),
      "/api/v1/cameras/c1/normalized-config/apply": () => json({ result: "ok", state: state({ desired_revision: 5, applied_revision: 5 }), review }),
    });
    await waitFor(() => expect(calls.some((c) => c.url.endsWith("normalized-config"))).toBe(true));
    await openDialog();
    expect(await screen.findByText("detección de vehículos: OFF → ON")).toBeInTheDocument();
    expect(screen.getByText("Frigate recargará configuración;")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Aplicar cambios" }));
    await screen.findByText("Cambios aplicados.");
    const applyCall = calls.find((c) => c.url.endsWith("/apply"));
    expect(applyCall?.body).toEqual({ patch: { detection: { enabled: true } }, expected_revision: 4, origin: "ui" });
    await waitFor(() => expect(screen.getByTestId("count")).toHaveTextContent("0"));
  });

  it("blocks applying when the engine cannot apply a field", async () => {
    mount({ "/api/v1/cameras/c1/normalized-config/review": () => json({ ...review, unsupported: ["recording.mode"] }) });
    await openDialog();
    expect(await screen.findByRole("alert")).toHaveTextContent("recording.mode");
    expect(screen.getByRole("button", { name: "Aplicar cambios" })).toBeDisabled();
  });

  it("keeps the draft and says so on a 409", async () => {
    mount({
      "/api/v1/cameras/c1/normalized-config/review": () => json(review),
      "/api/v1/cameras/c1/normalized-config/apply": () => json({ code: "conflict", message: "stale" }, 409),
    });
    await openDialog();
    await screen.findByText("detección de vehículos: OFF → ON");
    fireEvent.click(screen.getByRole("button", { name: "Aplicar cambios" }));
    expect(await screen.findByText(/La configuración cambió mientras editabas\./)).toBeInTheDocument();
    expect(screen.getByTestId("count")).toHaveTextContent("1");
  });

  it("shows a plain message and hides the engine error behind Ver información técnica", async () => {
    mount({
      "/api/v1/cameras/c1/normalized-config/review": () => json(review),
      "/api/v1/cameras/c1/normalized-config/apply": () => json({ result: "error", state: state({ sync_state: "error", last_error: "frigate: 500 ffmpeg exited" }), review }),
    });
    await openDialog();
    await screen.findByText("detección de vehículos: OFF → ON");
    fireEvent.click(screen.getByRole("button", { name: "Aplicar cambios" }));
    expect(await screen.findByText("No se pudo aplicar la configuración.")).toBeInTheDocument();
    expect(screen.queryByText(/ffmpeg exited/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Ver información técnica" }));
    expect(screen.getByText(/ffmpeg exited/)).toBeInTheDocument();
  });
});
