import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProvisionIngestionCard, type Ingestion } from "@/components/provision/ProvisionIngestionCard";
import { json, renderPage } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const serverID = "11111111-2222-3333-4444-555555555555";
const ingestionURL = `/api/v1/servers/${serverID}/provision-lpr/ingestion`;

function stub(body: Ingestion | { code: string; message: string }, status = 200) {
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const url = new URL(typeof input === "string" ? input : input instanceof Request ? input.url : input.toString(), "http://localhost");
    return url.pathname === ingestionURL ? json(body, status) : json({ code: "not_found", message: "nope" }, 404);
  }));
}

const healthy: Ingestion = {
  known: true, last_seq: 42, ingested_total: 40, skipped_total: 0, agent_lost: 0, agent_depth: 0,
  last_pull_at: "2026-10-24T12:00:00Z",
};

describe("ProvisionIngestionCard", () => {
  it("says nothing was collected yet when the server was never pulled", async () => {
    stub({ known: false, last_seq: 0, ingested_total: 0, skipped_total: 0, agent_lost: 0, agent_depth: 0 });
    renderPage(() => <ProvisionIngestionCard serverId={serverID} />);
    expect(await screen.findByText(/Todavía no se consultó el agente/)).toBeInTheDocument();
  });

  it("shows what was collected and when, without alarms when all is well", async () => {
    stub(healthy);
    renderPage(() => <ProvisionIngestionCard serverId={serverID} />);
    expect(await screen.findByText(/Guardadas en OpenVMS: 40/)).toBeInTheDocument();
    expect(screen.getByText(/Pendientes en el agente: 0/)).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.queryByText(/perdidas/i)).not.toBeInTheDocument();
  });

  it("warns about readings discarded and lost, and shows the last error", async () => {
    stub({ ...healthy, skipped_total: 4, agent_lost: 7, agent_depth: 12, last_error: "agent returned 401: unauthorized" });
    renderPage(() => <ProvisionIngestionCard serverId={serverID} />);
    expect(await screen.findByText(/Descartadas: 4/)).toBeInTheDocument();
    expect(screen.getByText(/7 lecturas se perdieron en el agente/)).toBeInTheDocument();
    expect(screen.getByText(/Pendientes en el agente: 12/)).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("agent returned 401: unauthorized");
  });

  it("asks to update an agent that predates the queue", async () => {
    stub(
      { known: true, last_seq: 0, ingested_total: 0, skipped_total: 0, agent_lost: 0, agent_depth: 0, last_error: "the agent does not support the Provision-ISR event queue; update the agent" },
    );
    renderPage(() => <ProvisionIngestionCard serverId={serverID} />);
    expect(await screen.findByRole("alert")).toHaveTextContent(/update the agent/);
  });

  it("reports a read failure instead of staying silent", async () => {
    stub({ code: "forbidden", message: "permission servers.manage required" }, 403);
    renderPage(() => <ProvisionIngestionCard serverId={serverID} />);
    expect(await screen.findByText(/permission servers.manage required/)).toBeInTheDocument();
  });
});
