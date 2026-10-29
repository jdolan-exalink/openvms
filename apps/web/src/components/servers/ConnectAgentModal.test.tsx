import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { ConnectAgentModal } from "@/components/servers/ConnectAgentModal";
import { json, renderPage } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const serverID = "bbbbbbbb-1111-2222-3333-444444444444";
const server = { id: serverID, name: "Sitio remoto" } as unknown as Schemas["Server"];
const connectionURL = `/api/v1/servers/${serverID}/agent/connection`;

type Call = { method: string; path: string; body?: Record<string, unknown>; csrf: string | null };

function stub(answer: () => Response) {
  const calls: Call[] = [];
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const req = input instanceof Request ? input : new Request(new URL(String(input), "http://localhost"), init);
    const url = new URL(req.url);
    if (url.pathname === connectionURL && req.method === "PUT") {
      calls.push({ method: req.method, path: url.pathname, body: JSON.parse(await req.clone().text()), csrf: req.headers.get("X-OpenVMS-Request") });
      return answer();
    }
    return json({ code: "not_found", message: "nope" }, 404);
  }));
  return calls;
}

const ok = () => json({ base_url: "https://agent.example.com:443", version: "0.4.0", scopes: { read: true, events: true, control: false } });

// The page renders after the router settles, so the first query has to wait for it.
async function fill(url: string, token: string) {
  fireEvent.change(await screen.findByLabelText(/^Dirección del agente/), { target: { value: url } });
  fireEvent.change(screen.getByLabelText(/^Token del agente/), { target: { value: token } });
}

describe("ConnectAgentModal", () => {
  it("sends the address and token, shows what the token can do, and forgets the token", async () => {
    const calls = stub(ok);
    renderPage(() => <ConnectAgentModal server={server} onClose={() => {}} />);
    await fill("https://agent.example.com", "ovmsa_secret");
    const token = screen.getByLabelText(/^Token del agente/) as HTMLInputElement;
    expect(token.type).toBe("password");
    fireEvent.click(screen.getByRole("button", { name: "Conectar" }));

    expect(await screen.findByText(/Versión 0\.4\.0/)).toBeInTheDocument();
    expect(calls).toHaveLength(1);
    expect(calls[0]?.csrf).not.toBeNull();
    expect(calls[0]?.body).toEqual({ url: "https://agent.example.com", token: "ovmsa_secret" });
    const list = screen.getByRole("list", { name: "Lo que permite el token" });
    expect(within(list).getByText(/Lectura \(estado/).textContent).toMatch(/✓/);
    expect(within(list).getByText(/Lecturas de patentes/).textContent).toMatch(/✓/);
    expect(within(list).getByText(/Control \(ONVIF/).textContent).toMatch(/✗/);
    expect(screen.getByRole("status")).toHaveTextContent(/Control/);
    expect((screen.queryByLabelText(/^Token del agente/) as HTMLInputElement | null)?.value ?? "").toBe("");
  });

  it.each([
    ["agent_unreachable", 502, /no pudo llegar/i],
    ["agent_token_rejected", 422, /rechazó el token/i],
    ["agent_token_scope", 422, /alcance de lectura/i],
    ["not_an_agent", 422, /no es un agente/i],
  ])("explains %s", async (code, status, message) => {
    stub(() => json({ code, message: "raw" }, status));
    renderPage(() => <ConnectAgentModal server={server} onClose={() => {}} />);
    await fill("http://10.8.0.5:7419", "t");
    fireEvent.click(screen.getByRole("button", { name: "Conectar" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(message);
    // The form stays: the operator fixes one field instead of retyping everything.
    expect((screen.getByLabelText(/^Dirección del agente/) as HTMLInputElement).value).toBe("http://10.8.0.5:7419");
  });

  it("asks before replacing an agent that is already registered and then sends the confirmation", async () => {
    let first = true;
    const calls = stub(() => {
      if (first) {
        first = false;
        return json({ code: "conflict", message: "agent is already registered or an install is active" }, 409);
      }
      return ok();
    });
    renderPage(() => <ConnectAgentModal server={server} onClose={() => {}} />);
    await fill("https://agent.example.com", "t");
    fireEvent.click(screen.getByRole("button", { name: "Conectar" }));
    const confirm = await screen.findByLabelText(/Reemplazar el agente registrado/);
    fireEvent.click(confirm);
    fireEvent.click(screen.getByRole("button", { name: "Conectar" }));
    await screen.findByText(/Versión 0\.4\.0/);
    expect(calls[0]?.body).not.toHaveProperty("replace_existing");
    expect(calls[1]?.body).toMatchObject({ replace_existing: true });
  });

  it("sends gateway headers and a certificate authority only when filled in", async () => {
    const calls = stub(ok);
    renderPage(() => <ConnectAgentModal server={server} onClose={() => {}} />);
    await fill("https://agent.example.com", "t");
    fireEvent.click(screen.getByText("Opciones avanzadas"));
    fireEvent.change(screen.getByLabelText("Nombre de la cabecera 1"), { target: { value: "CF-Access-Client-Id" } });
    fireEvent.change(screen.getByLabelText("Valor de la cabecera 1"), { target: { value: "abc.access" } });
    fireEvent.click(screen.getByRole("button", { name: "Agregar cabecera" }));
    // A row left empty is ignored, not sent.
    fireEvent.change(screen.getByLabelText(/^Certificado de la autoridad/), { target: { value: "-----BEGIN CERTIFICATE-----" } });
    fireEvent.click(screen.getByRole("button", { name: "Conectar" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]?.body).toEqual({
      url: "https://agent.example.com", token: "t",
      headers: { "CF-Access-Client-Id": "abc.access" }, ca_pem: "-----BEGIN CERTIFICATE-----",
    });
  });

  it("does not send an empty form", async () => {
    const calls = stub(ok);
    renderPage(() => <ConnectAgentModal server={server} onClose={() => {}} />);
    fireEvent.click(await screen.findByRole("button", { name: "Conectar" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/dirección y el token/i);
    expect(calls).toHaveLength(0);
  });

  it("auto-populates the agent URL using the host from server.base_url", async () => {
    const serverWithBase = { ...server, base_url: "http://10.1.1.252:5000" } as unknown as Schemas["Server"];
    renderPage(() => <ConnectAgentModal server={serverWithBase} onClose={() => {}} />);
    const input = (await screen.findByLabelText(/^Dirección del agente/)) as HTMLInputElement;
    expect(input.value).toBe("http://10.1.1.252:7419");
  });

  it("displays curl install command and apikey command with copy functionality", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    renderPage(() => <ConnectAgentModal server={server} onClose={() => {}} />);
    expect(await screen.findByText(/install-agent\.sh/)).toBeInTheDocument();
    expect(screen.getByText(/openvms\.apikey/)).toBeInTheDocument();

    const copyButtons = screen.getAllByRole("button", { name: /Copiar/i });
    expect(copyButtons.length).toBeGreaterThanOrEqual(2);
    fireEvent.click(copyButtons[0]!);
    expect(writeText).toHaveBeenCalledWith(expect.stringContaining("install-agent.sh"));
  });
});
