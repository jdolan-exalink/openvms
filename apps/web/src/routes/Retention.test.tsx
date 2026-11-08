import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage } from "@/test-utils";
import { Retention } from "./Retention";

afterEach(() => vi.unstubAllGlobals());

const view = (faceDays: number) => ({
  settings: { face_unknown_days: faceDays, notification_delivery_days: null, notification_read_days: 90, pressure_cleanup: true, pressure_percent: 85, updated_at: "2026-11-02T10:00:00Z" },
  volume: { measured: true, total_bytes: 1000 * 1024 ** 3, free_bytes: 400 * 1024 ** 3, percent: 60 },
  faces_pending: { records: 120, bytes: 5 * 1024 ** 2 },
});

function stub(opts: { faceDays?: number; previewRecords?: number; onPut?: (body: unknown) => void; onRulePut?: (body: unknown) => void; forbidden?: boolean } = {}) {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request) => {
      const url = new URL(input.url);
      calls.push(`${input.method} ${url.pathname}${url.search}`);
      if (opts.forbidden) return json({ code: "forbidden", message: "you do not have permission for this action" }, 403);
      if (url.pathname === "/api/v1/system/faces/recurrence" && input.method === "PUT") {
        const body = await input.json();
        opts.onRulePut?.(body);
        return json({ ...(body as object), updated_at: "2026-11-08T10:00:00Z" });
      }
      if (url.pathname === "/api/v1/system/faces/recurrence") return json({ min_appearances: 3, window_days: 7, updated_at: "2026-11-02T10:00:00Z" });
      if (url.pathname === "/api/v1/system/retention/preview") return json({ records: opts.previewRecords ?? 0, bytes: 2048 });
      if (url.pathname === "/api/v1/system/retention" && input.method === "PUT") {
        const body = await input.json();
        opts.onPut?.(body);
        return json({ ...view(30).settings, ...(body as object) });
      }
      if (url.pathname === "/api/v1/system/retention") return json(view(opts.faceDays ?? 30));
      return json({ code: "not_found", message: "not found" }, 404);
    }),
  );
  return calls;
}

describe("Retention", () => {
  it("shows the recurrence rule and saves a new one on its own", async () => {
    let rule: unknown;
    let put: unknown;
    stub({ onRulePut: (b) => (rule = b), onPut: (b) => (put = b) });
    renderPage(Retention);
    expect(await screen.findByLabelText("Apariciones para marcar como recurrente")).toHaveValue(3);
    expect(screen.getByLabelText("En los últimos (días)")).toHaveValue(7);
    fireEvent.change(screen.getByLabelText("Apariciones para marcar como recurrente"), { target: { value: "5" } });
    fireEvent.change(screen.getByLabelText("En los últimos (días)"), { target: { value: "14" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar regla" }));
    await waitFor(() => expect(rule).toEqual({ min_appearances: 5, window_days: 14 }));
    expect(await screen.findByText("Regla guardada.")).toBeInTheDocument();
    expect(put).toBeUndefined(); // the retention periods are not sent along
  });

  it("shows the current periods, the volume and the faces stored", async () => {
    stub();
    renderPage(Retention);
    expect(await screen.findByLabelText("Rostros sin nombre · Cantidad de días")).toHaveValue(30);
    expect(screen.getByLabelText("Notificaciones leídas · Cantidad de días")).toHaveValue(90);
    expect(screen.getByLabelText("Envíos entregados")).toHaveValue("default");
    expect(screen.getByText(/60% usado/)).toBeInTheDocument();
    expect(screen.getByText(/Rostros sin nombre guardados: 120/)).toBeInTheDocument();
  });

  it("saves a longer period straight away, without a preview", async () => {
    let put: unknown;
    const calls = stub({ onPut: (b) => (put = b) });
    renderPage(Retention);
    fireEvent.change(await screen.findByLabelText("Rostros sin nombre · Cantidad de días"), { target: { value: "60" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(put).toEqual({ face_unknown_days: 60, notification_delivery_days: null, notification_read_days: 90, pressure_cleanup: true, pressure_percent: 85 }));
    expect(calls.some((c) => c.includes("/preview"))).toBe(false);
  });

  it("asks before shortening a period that would erase faces, then saves", async () => {
    let put: unknown;
    const calls = stub({ previewRecords: 40, onPut: (b) => (put = b) });
    renderPage(Retention);
    fireEvent.change(await screen.findByLabelText("Rostros sin nombre · Cantidad de días"), { target: { value: "7" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect(await screen.findByText(/se van a borrar 40 rostros sin nombre/)).toBeInTheDocument();
    expect(calls).toContain("GET /api/v1/system/retention/preview?face_unknown_days=7");
    expect(put).toBeUndefined();
    fireEvent.click(screen.getByRole("button", { name: "Borrar y guardar" }));
    await waitFor(() => expect(put).toMatchObject({ face_unknown_days: 7 }));
  });

  it("saves a shorter period without a prompt when it would erase nothing", async () => {
    let put: unknown;
    stub({ previewRecords: 0, onPut: (b) => (put = b) });
    renderPage(Retention);
    fireEvent.change(await screen.findByLabelText("Rostros sin nombre · Cantidad de días"), { target: { value: "7" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(put).toMatchObject({ face_unknown_days: 7 }));
    expect(screen.queryByText(/se van a borrar/)).not.toBeInTheDocument();
  });

  it("keeps forever as 0 and the server default as null", async () => {
    let put: unknown;
    stub({ onPut: (b) => (put = b) });
    renderPage(Retention);
    fireEvent.change(await screen.findByLabelText("Rostros sin nombre"), { target: { value: "forever" } });
    fireEvent.change(screen.getByLabelText("Notificaciones leídas"), { target: { value: "default" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(put).toMatchObject({ face_unknown_days: 0, notification_read_days: null }));
  });

  it("shows the refusal to someone without the platform permission", async () => {
    stub({ forbidden: true });
    renderPage(Retention);
    expect(await screen.findByText(/no tenés permiso|permission/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Guardar" })).not.toBeInTheDocument();
  });
});
