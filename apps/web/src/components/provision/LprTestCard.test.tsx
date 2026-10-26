import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LprTestCard } from "@/components/provision/LprTestCard";
import { json } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const cameraID = "aaaaaaaa-1111-2222-3333-444444444444";

function renderCard(engine?: "provision" | "frigate" | "none") {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <LprTestCard cameraId={cameraID} engine={engine} />
    </QueryClientProvider>,
  );
}

type Sent = { path: string; method: string; body: Record<string, unknown>; csrf: string | null };

function stub(respond: () => Promise<Response> | Response) {
  const sent: Sent[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(typeof input === "string" ? input : input instanceof Request ? input.url : input.toString(), "http://localhost");
      sent.push({ path: url.pathname, method: init?.method ?? "GET", body: JSON.parse(typeof init?.body === "string" ? init.body : "{}"), csrf: new Headers(init?.headers).get("X-OpenVMS-Request") });
      return respond();
    }),
  );
  return sent;
}

const allPass = {
  engine: "provision", passed: true, hints: ["La cámara debe llamar a http://10.1.1.5:7419/SendKeepalive (keepalive)."],
  checks: [
    { id: "agent_reachable", status: "pass", message: "OpenVMS alcanza al agente" },
    { id: "keepalive_recent", status: "pass", message: "La cámara está enviando keepalives al agente", detail: "Último keepalive: 2026-10-26T12:00:00Z." },
  ],
  observed: { keepalive_count: 40, event_count: 3, last_plate: "AB123CD" },
};

describe("LprTestCard", () => {
  it("starts idle: a button and no result, nothing sent", () => {
    const sent = stub(() => json(allPass));
    renderCard("provision");
    expect(screen.getByRole("button", { name: "Probar conexión" })).toBeEnabled();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(sent).toHaveLength(0);
  });

  it("waits for a keepalive by default for a Provision camera and shows that it is waiting", async () => {
    let release: (r: Response) => void = () => {};
    const sent = stub(() => new Promise<Response>((r) => (release = r)));
    renderCard("provision");
    fireEvent.click(screen.getByRole("button", { name: "Probar conexión" }));
    expect(await screen.findByRole("button", { name: "Esperando keepalive…" })).toBeDisabled();
    expect(sent[0]).toMatchObject({ path: `/api/v1/cameras/${cameraID}/lpr/test`, method: "POST", csrf: "1", body: { wait_seconds: 20 } });
    release(json(allPass));
    expect(await screen.findByText("La conexión funciona.")).toBeInTheDocument();
  });

  it("does not wait when the box is cleared or the camera is read by Frigate", async () => {
    const sent = stub(() => json(allPass));
    const { unmount } = renderCard("provision");
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "Probar conexión" }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]?.body).toEqual({ wait_seconds: 0 });
    unmount();

    renderCard("frigate");
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Probar conexión" }));
    await waitFor(() => expect(sent).toHaveLength(2));
    expect(sent[1]?.body).toEqual({ wait_seconds: 0 });
  });

  it("lists every check, the hints and the observed counters when all pass", async () => {
    stub(() => json(allPass));
    renderCard("provision");
    fireEvent.click(screen.getByRole("button", { name: "Probar conexión" }));
    expect(await screen.findByText("La conexión funciona.")).toBeInTheDocument();
    expect(screen.getByText("OpenVMS alcanza al agente")).toBeInTheDocument();
    expect(screen.getByText(/Último keepalive: 2026-10-26/)).toBeInTheDocument();
    expect(screen.getByText(/10\.1\.1\.5:7419\/SendKeepalive/)).toBeInTheDocument();
    expect(screen.getByText("AB123CD")).toBeInTheDocument();
    expect(screen.getByText("40")).toBeInTheDocument();
    expect(document.querySelectorAll('li[data-status="pass"]')).toHaveLength(2);
  });

  it("shows a failing check with what to do about it", async () => {
    stub(() =>
      json({
        engine: "provision", passed: false, hints: ["Registrá la cámara con Configurar y cargá los valores en la cámara"],
        checks: [{ id: "registered", status: "fail", message: "La cámara no está registrada en el agente", detail: "Registrá la cámara con Configurar y cargá los valores en la cámara." }],
        observed: {},
      }),
    );
    renderCard("provision");
    fireEvent.click(screen.getByRole("button", { name: "Probar conexión" }));
    expect(await screen.findByText("Hay pasos que fallan.")).toBeInTheDocument();
    expect(screen.getByText("La cámara no está registrada en el agente")).toBeInTheDocument();
    expect(screen.getByText("Registrá la cámara con Configurar y cargá los valores en la cámara")).toBeInTheDocument();
    expect(document.querySelector('li[data-status="fail"]')).not.toBeNull();
  });

  it("shows a warning without calling the test a failure", async () => {
    stub(() =>
      json({
        engine: "provision", passed: true, hints: [],
        checks: [{ id: "event_seen", status: "warn", message: "La cámara está conectada; falta una lectura de patente" }],
        observed: {},
      }),
    );
    renderCard("provision");
    fireEvent.click(screen.getByRole("button", { name: "Probar conexión" }));
    expect(await screen.findByText("La conexión funciona.")).toBeInTheDocument();
    expect(document.querySelector('li[data-status="warn"]')).not.toBeNull();
    expect(screen.getByText("La cámara está conectada; falta una lectura de patente")).toBeInTheDocument();
  });

  it("shows the error when the test itself fails and lets the user retry", async () => {
    stub(() => json({ code: "forbidden", message: "you do not have permission for this action" }, 403));
    renderCard("provision");
    fireEvent.click(screen.getByRole("button", { name: "Probar conexión" }));
    expect(await screen.findByText(/permission/)).toBeInTheDocument();
    expect(screen.queryByText("La conexión funciona.")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Probar conexión" })).toBeEnabled();
  });

  it("says the camera does not read plates when the engine is none", async () => {
    stub(() => json({ engine: "none", passed: true, hints: [], checks: [{ id: "not_lpr", status: "skip", message: "La cámara no es LPR" }], observed: {} }));
    renderCard("none");
    fireEvent.click(screen.getByRole("button", { name: "Probar conexión" }));
    expect(await screen.findByText("Esta cámara no lee patentes.")).toBeInTheDocument();
    expect(screen.getByText("La cámara no es LPR")).toBeInTheDocument();
    expect(document.querySelector('li[data-status="skip"]')).not.toBeNull();
  });

  it("shows the Frigate counters", async () => {
    stub(() =>
      json({ engine: "frigate", passed: true, hints: [], checks: [{ id: "reads_recent", status: "pass", message: "La cámara tiene lecturas de patente en las últimas 24 horas" }], observed: { reads_24h: 7, last_read_at: "2026-10-26T11:00:00Z" } }),
    );
    renderCard("frigate");
    fireEvent.click(screen.getByRole("button", { name: "Probar conexión" }));
    expect(await screen.findByText("Lecturas en 24 h")).toBeInTheDocument();
    expect(screen.getByText("7")).toBeInTheDocument();
  });
});
