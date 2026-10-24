import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProvisionRegistryCard, type Registry } from "@/components/provision/ProvisionRegistryCard";
import { json, renderPage } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const serverID = "11111111-2222-3333-4444-555555555555";
const registryURL = `/api/v1/servers/${serverID}/provision-lpr/registry`;
const enforceURL = `/api/v1/servers/${serverID}/provision-lpr/enforce`;

const open: Registry = {
  enforce: false,
  cameras: [{ camera_id: "cam-a", ip: "10.0.15.107" }],
  unregistered_seen: [{ ip: "10.0.15.200", last_seen: "2026-10-24T10:05:00Z", requests: 3, last_path: "/Vendor/Event", rejected: false }],
};

type Call = { method: string; path: string; body?: string; csrf: string | null };

function stubRegistry(initial: Registry | (() => Response)) {
  const calls: Call[] = [];
  let current = typeof initial === "function" ? null : initial;
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(typeof input === "string" ? input : input instanceof Request ? input.url : input.toString(), "http://localhost");
    const method = init?.method ?? (input instanceof Request ? input.method : "GET");
    const csrf = new Headers(init?.headers).get("X-OpenVMS-Request");
    calls.push({ method, path: url.pathname, body: typeof init?.body === "string" ? init.body : undefined, csrf });
    if (typeof initial === "function") return initial();
    if (url.pathname === registryURL) return json(current);
    if (url.pathname === enforceURL && method === "PUT") {
      current = { ...open, ...(current ?? open), enforce: JSON.parse(String(init?.body)).enforce };
      return json(current);
    }
    return json({ code: "not_found", message: "nope" }, 404);
  }));
  return calls;
}

describe("ProvisionRegistryCard", () => {
  it("lists the addresses that sent data without being registered and says the receiver is open", async () => {
    stubRegistry(open);
    renderPage(() => <ProvisionRegistryCard serverId={serverID} />);
    expect(await screen.findByText(/Sin exigir/)).toBeInTheDocument();
    expect(screen.getByText("10.0.15.200")).toBeInTheDocument();
    expect(screen.getByText(/3 pedidos/)).toBeInTheDocument();
    expect(screen.getByText(/Registradas: 1/)).toBeInTheDocument();
  });

  it("asks before enforcing, names the addresses that will stop being accepted, and then enforces", async () => {
    const calls = stubRegistry(open);
    renderPage(() => <ProvisionRegistryCard serverId={serverID} />);
    fireEvent.click(await screen.findByRole("button", { name: "Exigir cámaras registradas" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/1 dirección que hoy no están registradas: 10\.0\.15\.200/)).toBeInTheDocument();
    expect(calls.some((c) => c.method === "PUT")).toBe(false);

    fireEvent.click(within(dialog).getByRole("button", { name: "Exigir" }));
    await waitFor(() => expect(calls).toContainEqual({ method: "PUT", path: enforceURL, body: '{"enforce":true}', csrf: "1" }));
    expect(await screen.findByText(/Exigido: solo las cámaras registradas/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Dejar de exigir cámaras registradas" })).toBeInTheDocument();
  });

  it("does not enforce when the confirmation is cancelled", async () => {
    const calls = stubRegistry(open);
    renderPage(() => <ProvisionRegistryCard serverId={serverID} />);
    fireEvent.click(await screen.findByRole("button", { name: "Exigir cámaras registradas" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: /Cancel|Cancelar/ }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(calls.some((c) => c.method === "PUT")).toBe(false);
  });

  it("turns enforcement off without asking", async () => {
    const calls = stubRegistry({ ...open, enforce: true });
    renderPage(() => <ProvisionRegistryCard serverId={serverID} />);
    fireEvent.click(await screen.findByRole("button", { name: "Dejar de exigir cámaras registradas" }));
    await waitFor(() => expect(calls).toContainEqual({ method: "PUT", path: enforceURL, body: '{"enforce":false}', csrf: "1" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("marks the addresses that enforcement turned away", async () => {
    stubRegistry({ ...open, enforce: true, unregistered_seen: [{ ...open.unregistered_seen[0]!, rejected: true }] });
    renderPage(() => <ProvisionRegistryCard serverId={serverID} />);
    expect(await screen.findByText("rechazada")).toBeInTheDocument();
  });

  it("tells an old agent from a failure", async () => {
    stubRegistry(() => json({ code: "agent_outdated", message: "update the agent" }, 409));
    renderPage(() => <ProvisionRegistryCard serverId={serverID} />);
    expect(await screen.findByText(/anterior al registro de cámaras/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Exigir/ })).not.toBeInTheDocument();
  });

  it("shows other failures and does not offer to enforce", async () => {
    stubRegistry(() => json({ code: "agent_unreachable", message: "the agent could not be reached" }, 502));
    renderPage(() => <ProvisionRegistryCard serverId={serverID} />);
    expect(await screen.findByText(/the agent could not be reached/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Exigir/ })).not.toBeInTheDocument();
  });

  it("warns that an unreadable registry refuses every camera and blocks changing it from here", async () => {
    stubRegistry({ ...open, enforce: true, load_error: "invalid character" });
    renderPage(() => <ProvisionRegistryCard serverId={serverID} />);
    expect(await screen.findByRole("alert")).toHaveTextContent(/rechaza todas las cámaras/);
    expect(screen.getByRole("button", { name: "Dejar de exigir cámaras registradas" })).toBeDisabled();
  });
});
