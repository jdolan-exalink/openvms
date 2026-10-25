import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { ConnectAgentControls } from "@/components/servers/ConnectAgentControls";
import { json, renderPage } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const serverID = "bbbbbbbb-1111-2222-3333-444444444444";
const server = { id: serverID, name: "Sitio remoto" } as unknown as Schemas["Server"];
const connectionURL = `/api/v1/servers/${serverID}/agent/connection`;

function stub(state: Record<string, unknown>) {
  const calls: { method: string; csrf: string | null }[] = [];
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const req = input instanceof Request ? input : new Request(new URL(String(input), "http://localhost"), init);
    const url = new URL(req.url);
    if (url.pathname === connectionURL) {
      calls.push({ method: req.method, csrf: req.headers.get("X-OpenVMS-Request") });
      if (req.method === "DELETE") return new Response(null, { status: 204 });
      return json(state);
    }
    return json({ code: "not_found", message: "nope" }, 404);
  }));
  return calls;
}

describe("ConnectAgentControls", () => {
  it("offers to connect an agent when the server has none connected by address", async () => {
    stub({ connected: false, has_headers: false, has_custom_ca: false });
    renderPage(() => <ConnectAgentControls server={server} />);
    fireEvent.click(await screen.findByRole("button", { name: "Conectar agente existente" }));
    expect(await screen.findByRole("dialog", { name: /Conectar agente/ })).toBeInTheDocument();
  });

  it("shows where a connected agent is reached, without any secret, and disconnects after confirming", async () => {
    const calls = stub({ connected: true, base_url: "https://agent.example.com:443", has_headers: true, has_custom_ca: false });
    renderPage(() => <ConnectAgentControls server={server} />);
    expect(await screen.findByText("https://agent.example.com:443")).toBeInTheDocument();
    expect(screen.getByText(/cabeceras de gateway/i)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Desconectar" }));
    fireEvent.click(await screen.findByRole("button", { name: "Desconectar agente" }));
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE")).toBe(true));
    expect(calls.find((c) => c.method === "DELETE")?.csrf).not.toBeNull();
  });

  it("says nothing alarming when the connection state cannot be read", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => json({ code: "forbidden", message: "no" }, 403)));
    renderPage(() => <ConnectAgentControls server={server} />);
    expect(await screen.findByRole("button", { name: "Conectar agente existente" })).toBeInTheDocument();
  });
});
