import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { CameraWebView } from "./CameraWebView";

afterEach(() => vi.unstubAllGlobals());

const URL_PATH = "/api/v1/cameras/cam1/web/session";
const session = { url: "/camproxy/" + "a".repeat(64) + "/", expires_at: "2026-10-26T12:30:00Z" };

async function setup(routes: Record<string, () => Response>) {
  const fetchMock = vi.fn(stubApi(routes));
  vi.stubGlobal("fetch", fetchMock);
  renderPage(() => <CameraWebView cameraId="cam1" />);
  await screen.findByRole("button", { name: "Abrir" });
  return fetchMock;
}

const sessionRequests = (fetchMock: ReturnType<typeof vi.fn>) =>
  fetchMock.mock.calls.map(([r]) => r as Request).filter((r) => r.url.endsWith(URL_PATH));

describe("CameraWebView", () => {
  it("starts idle: no request, no iframe", async () => {
    const fetchMock = await setup({});
    expect(screen.getByRole("button", { name: "Abrir" })).toBeEnabled();
    expect(screen.queryByTitle("Web de la cámara")).not.toBeInTheDocument();
    expect(sessionRequests(fetchMock)).toHaveLength(0);
  });

  it("shows a creating state while the session is being made", async () => {
    let release: (r: Response) => void = () => undefined;
    vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>((resolve) => { release = resolve; })));
    renderPage(() => <CameraWebView cameraId="cam1" />);
    fireEvent.click(await screen.findByRole("button", { name: "Abrir" }));
    expect(await screen.findByRole("button", { name: "Abriendo…" })).toBeDisabled();
    expect(screen.getByRole("status")).toHaveTextContent(/Conectando/);
    expect(screen.queryByTitle("Web de la cámara")).not.toBeInTheDocument();
    release(json(session));
    expect(await screen.findByTitle("Web de la cámara")).toBeInTheDocument();
  });

  it("opens with the default http 80 and renders a sandbox without allow-same-origin", async () => {
    const fetchMock = await setup({ [`POST ${URL_PATH}`]: () => json(session) });
    fireEvent.click(screen.getByRole("button", { name: "Abrir" }));
    const frame = await screen.findByTitle("Web de la cámara");
    expect(frame).toHaveAttribute("src", session.url);
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts allow-forms allow-popups allow-modals");
    expect(frame.getAttribute("sandbox")).not.toContain("allow-same-origin");
    const [req] = sessionRequests(fetchMock);
    expect(await req?.json()).toEqual({ scheme: "http", port: 80 });
    const link = screen.getByRole("link", { name: "Abrir en otra pestaña" });
    expect(link).toHaveAttribute("href", session.url);
    expect(link).toHaveAttribute("target", "_blank");
    expect(link.getAttribute("rel")).toContain("noopener");
    expect(screen.getByRole("button", { name: "Recargar" })).toBeInTheDocument();
  });

  it("sends the chosen preset and a custom scheme and port", async () => {
    const fetchMock = await setup({ [`POST ${URL_PATH}`]: () => json(session) });
    fireEvent.change(screen.getByLabelText("Puerto de la web de la cámara"), { target: { value: "https:443" } });
    fireEvent.click(screen.getByRole("button", { name: "Abrir" }));
    await screen.findByTitle("Web de la cámara");
    expect(await sessionRequests(fetchMock)[0]?.json()).toEqual({ scheme: "https", port: 443 });

    fireEvent.change(screen.getByLabelText("Puerto de la web de la cámara"), { target: { value: "custom" } });
    expect(screen.getByRole("button", { name: "Abrir" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Puerto personalizado"), { target: { value: "99999" } });
    expect(screen.getByRole("button", { name: "Abrir" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Protocolo"), { target: { value: "https" } });
    fireEvent.change(screen.getByLabelText("Puerto personalizado"), { target: { value: "8443" } });
    fireEvent.click(screen.getByRole("button", { name: "Abrir" }));
    await waitFor(() => expect(sessionRequests(fetchMock)).toHaveLength(2));
    expect(await sessionRequests(fetchMock)[1]?.json()).toEqual({ scheme: "https", port: 8443 });
  });

  it.each([
    [409, "agent_unprovisioned", /no tiene un agente/],
    [409, "agent_unsupported", /agente de este servidor es antiguo/],
    [404, "camera_address_unknown", /dirección de la cámara/],
    [502, "camera_unreachable", /Revisá la dirección y el puerto de la cámara/],
    [403, "forbidden", /No tenés permiso/],
  ])("explains a %i %s answer and shows no iframe", async (status, code, text) => {
    await setup({ [`POST ${URL_PATH}`]: () => json({ code, message: "x" }, status) });
    fireEvent.click(screen.getByRole("button", { name: "Abrir" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(text);
    expect(screen.queryByTitle("Web de la cámara")).not.toBeInTheDocument();
  });

  it("reloads the frame without creating another session", async () => {
    const fetchMock = await setup({ [`POST ${URL_PATH}`]: () => json(session) });
    fireEvent.click(screen.getByRole("button", { name: "Abrir" }));
    const first = await screen.findByTitle("Web de la cámara");
    fireEvent.click(screen.getByRole("button", { name: "Recargar" }));
    await waitFor(() => expect(screen.getByTitle("Web de la cámara")).not.toBe(first));
    expect(sessionRequests(fetchMock)).toHaveLength(1);
  });
});
