import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { json, renderPage } from "@/test-utils";
import { Alarms } from "./Alarms";

afterEach(() => vi.unstubAllGlobals());

function makeAlarm(id: string, overrides: Partial<Schemas["Alarm"]> = {}): Schemas["Alarm"] {
  return {
    id,
    tenant_id: "t1",
    site_id: "s1",
    site_name: "Sitio Centro",
    camera_id: "c1",
    camera_name: "Cámara Acceso",
    event_id: "e1",
    event_severity: "alert",
    event_start_time: "2026-09-29T10:00:00Z",
    event_labels: ["person"],
    event_sub_labels: [],
    source: "event",
    status: "open",
    created_at: "2026-09-29T10:00:00Z",
    updated_at: "2026-09-29T10:00:00Z",
    ...overrides,
  };
}

describe("Alarms Route", () => {
  it("renders empty state when there are no alarms", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (req: Request) => {
        const url = new URL(req.url);
        if (url.pathname === "/api/v1/alarms") return json({ items: [] });
        if (url.pathname === "/api/v1/cameras") return json({ items: [] });
        return json({}, 404);
      }),
    );

    renderPage(() => <Alarms />);

    expect(await screen.findByText(/No hay alarmas para el filtro seleccionado/i)).toBeInTheDocument();
  });

  it("renders alarms table with badges and details", async () => {
    const alarm1 = makeAlarm("a1", {
      status: "open",
      camera_name: "Cámara Principal",
      event_labels: ["person", "vehicle"],
    });

    vi.stubGlobal(
      "fetch",
      vi.fn(async (req: Request) => {
        const url = new URL(req.url);
        if (url.pathname === "/api/v1/alarms") return json({ items: [alarm1] });
        if (url.pathname === "/api/v1/cameras") return json({ items: [] });
        return json({}, 404);
      }),
    );

    renderPage(() => <Alarms />);

    expect(await screen.findByText("Cámara Principal")).toBeInTheDocument();
    expect(screen.getByText("Persona, vehicle")).toBeInTheDocument();
    expect(screen.getByText("Clasificación:")).toBeInTheDocument();
    expect(screen.getByText("Abierta")).toBeInTheDocument();
    expect(screen.getByText("Sin asignar")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Reconocer/i })).toBeInTheDocument();
  });

  it("acknowledges an open alarm", async () => {
    let ackCalled = false;
    const alarm = makeAlarm("a1", { status: "open" });

    vi.stubGlobal(
      "fetch",
      vi.fn(async (req: Request) => {
        const url = new URL(req.url);
        if (url.pathname === "/api/v1/alarms") {
          return json({ items: ackCalled ? [{ ...alarm, status: "acknowledged" }] : [alarm] });
        }
        if (url.pathname === "/api/v1/alarms/a1/acknowledge" && req.method === "POST") {
          ackCalled = true;
          return json({ ...alarm, status: "acknowledged" });
        }
        if (url.pathname === "/api/v1/cameras") return json({ items: [] });
        return json({}, 404);
      }),
    );

    renderPage(() => <Alarms />);

    const ackBtn = await screen.findByRole("button", { name: "Reconocer alarma a1" });
    fireEvent.click(ackBtn);

    await waitFor(() => {
      expect(ackCalled).toBe(true);
    });
  });

  it("resolves an alarm", async () => {
    let resolveCalled = false;
    const alarm = makeAlarm("a1", { status: "open" });

    vi.stubGlobal(
      "fetch",
      vi.fn(async (req: Request) => {
        const url = new URL(req.url);
        if (url.pathname === "/api/v1/alarms") {
          return json({ items: resolveCalled ? [] : [alarm] });
        }
        if (url.pathname === "/api/v1/alarms/a1/resolve" && req.method === "POST") {
          resolveCalled = true;
          return json({ ...alarm, status: "resolved" });
        }
        if (url.pathname === "/api/v1/cameras") return json({ items: [] });
        return json({}, 404);
      }),
    );

    renderPage(() => <Alarms />);

    const resolveBtn = await screen.findByRole("button", { name: "Resolver alarma a1" });
    fireEvent.click(resolveBtn);

    await waitFor(() => {
      expect(resolveCalled).toBe(true);
    });
  });

  it("opens assign modal, loads assignees, and assigns user", async () => {
    let assignCalledWith: { alarmId: string; userId: string } | null = null;
    const alarm = makeAlarm("a1", { status: "open", camera_name: "Cámara Acceso" });
    const users = [
      { id: "u1", username: "operador", display_name: "Juan Operador" },
    ];

    vi.stubGlobal(
      "fetch",
      vi.fn(async (req: Request) => {
        const url = new URL(req.url);
        if (url.pathname === "/api/v1/alarms") return json({ items: [alarm] });
        if (url.pathname === "/api/v1/alarms/a1/assignees") return json({ items: users });
        if (url.pathname === "/api/v1/alarms/a1/assign" && req.method === "POST") {
          const body = await req.json();
          assignCalledWith = { alarmId: "a1", userId: body.user_id };
          return json({ ...alarm, assigned_to: body.user_id });
        }
        if (url.pathname === "/api/v1/cameras") return json({ items: [] });
        return json({}, 404);
      }),
    );

    renderPage(() => <Alarms />);

    const assignBtn = await screen.findByRole("button", { name: "Asignar alarma a1" });
    fireEvent.click(assignBtn);

    expect(await screen.findByText(/Asignar alarma en Cámara Acceso/i)).toBeInTheDocument();
    expect(await screen.findByText("Juan Operador (operador)")).toBeInTheDocument();

    const select = screen.getByRole("combobox");
    fireEvent.change(select, { target: { value: "u1" } });

    const saveBtn = screen.getByRole("button", { name: "Guardar asignación" });
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(assignCalledWith).toEqual({ alarmId: "a1", userId: "u1" });
    });
  });

  it("handles bulk selection and action", async () => {
    let bulkActionPayload: { action: string; alarm_ids: string[] } | null = null;
    const a1 = makeAlarm("a1");
    const a2 = makeAlarm("a2");

    vi.stubGlobal(
      "fetch",
      vi.fn(async (req: Request) => {
        const url = new URL(req.url);
        if (url.pathname === "/api/v1/alarms") return json({ items: [a1, a2] });
        if (url.pathname === "/api/v1/alarms/bulk" && req.method === "POST") {
          bulkActionPayload = await req.json();
          return json({ updated: 2 });
        }
        if (url.pathname === "/api/v1/cameras") return json({ items: [] });
        return json({}, 404);
      }),
    );

    renderPage(() => <Alarms />);

    expect(await screen.findByRole("table", { name: "Bandeja de alarmas" })).toBeInTheDocument();

    // Select all
    const selectAllCheckbox = screen.getByLabelText("Seleccionar todas las alarmas");
    fireEvent.click(selectAllCheckbox);

    expect(screen.getByText("2 alarmas seleccionadas")).toBeInTheDocument();

    // Click bulk acknowledge
    const bulkAckBtn = screen.getByRole("button", { name: /Reconocer seleccionadas/i });
    fireEvent.click(bulkAckBtn);

    await waitFor(() => {
      expect(bulkActionPayload).toEqual({
        action: "acknowledge",
        alarm_ids: ["a1", "a2"],
      });
    });
  });
});
