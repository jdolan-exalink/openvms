import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { setLocale } from "@/i18n";
import { AgentJobProvider, useAgentJobs } from "@/lib/agentJobs/AgentJobProvider";
import { AgentJobStatusRegion } from "./AgentJobStatusRegion";

const queuedInstall = {
  id: "job-1",
  status: "queued" as const,
  stage: "validating" as const,
  message: "Install queued",
};

const queuedUpdate = { ...queuedInstall, id: "update-1" };

function JobPage({ kind = "install" }: { kind?: "install" | "update" }) {
  const { registerJob } = useAgentJobs();
  const [route, setRoute] = useState("servers");
  if (route !== "servers") return <p>Another app page</p>;
  return <button onClick={() => {
    registerJob({
      kind,
      serverId: "server-1",
      serverName: "North lab",
      job: kind === "install" ? queuedInstall : queuedUpdate,
    });
    setRoute("another");
  }}>Start tracking job</button>;
}

function renderJobShell(response: Response | Error, kind: "install" | "update" = "install") {
  const respond = () => {
    if (response instanceof Error) throw response;
    return response;
  };
  vi.stubGlobal("fetch", vi.fn(stubApi({
    "GET /api/v1/servers/server-1/agent/install/job-1": respond,
    "GET /api/v1/servers/server-1/agent/update-ssh/update-1": respond,
  })));
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <AgentJobProvider>
        <AgentJobStatusRegion />
        <JobPage kind={kind} />
      </AgentJobProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  setLocale("es");
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("AgentJobStatusRegion", () => {
  it("shows active work in the app shell after the originating page changes", async () => {
    setLocale("en");
    const response = json({
      ...queuedInstall,
      status: "running",
      stage: "transferring",
      message: "Transferring agent files over SFTP",
    });
    renderJobShell(response);

    fireEvent.click(screen.getByRole("button", { name: "Start tracking job" }));

    expect(await screen.findByText("Transferring files")).toBeInTheDocument();
    expect(screen.getByText("North lab")).toBeInTheDocument();
  });

  it("announces one authoritative install success without claiming HTTPS or camera readiness", async () => {
    setLocale("en");
    const { container } = renderJobShell(json({ ...queuedInstall, status: "succeeded", stage: "complete" }));

    fireEvent.click(screen.getByRole("button", { name: "Start tracking job" }));

    const notice = await screen.findByText(/Agent files were installed and registration completed/i);
    expect(notice).toHaveTextContent(/HTTPS health is not verified/i);
    expect(notice).not.toHaveTextContent(/camera ready/i);
    expect(container.querySelectorAll("[data-agent-job-notice]")).toHaveLength(1);
    await waitFor(() => expect(screen.getByText("North lab")).toBeInTheDocument());
    expect(container.querySelectorAll("[data-agent-job-notice]")).toHaveLength(1);
  });

  it("distinguishes verified update health from camera readiness", async () => {
    setLocale("en");
    const { container } = renderJobShell(json({ ...queuedUpdate, status: "succeeded", stage: "complete" }), "update");

    fireEvent.click(screen.getByRole("button", { name: "Start tracking job" }));

    const notice = await screen.findByText(/authenticated HTTPS health and binary identity were verified/i);
    expect(notice).toHaveTextContent(/Camera readiness is not established/i);
    expect(container.querySelectorAll("[data-agent-job-notice]")).toHaveLength(1);
  });

  it("renders a safe failure notice and never exposes hostile server text", async () => {
    setLocale("en");
    const { container } = renderJobShell(json({
      ...queuedInstall,
      status: "failed",
      stage: "failed",
      message: "root-secret password and remote trace",
    }));

    fireEvent.click(screen.getByRole("button", { name: "Start tracking job" }));

    expect(await screen.findByText("Agent installation failed.")).toBeInTheDocument();
    expect(container).not.toHaveTextContent("root-secret");
    expect(container.querySelectorAll("[data-agent-job-notice]")).toHaveLength(1);
  });

  it.each([401, 403, 404])("shows unknown for an unavailable job after HTTP %s without announcing success or failure", async (status) => {
    setLocale("en");
    const { container } = renderJobShell(json({ code: "unavailable", message: "root-secret" }, status));

    fireEvent.click(screen.getByRole("button", { name: "Start tracking job" }));

    expect(await screen.findByText("The outcome could not be confirmed. Check Servers before retrying.")).toBeInTheDocument();
    expect(container).not.toHaveTextContent("root-secret");
    expect(container.querySelectorAll("[data-agent-job-notice]")).toHaveLength(0);
  });

  it("shows unknown after a network failure without converting the active snapshot into a result", async () => {
    setLocale("en");
    const { container } = renderJobShell(new Error("root-secret network failure"));

    fireEvent.click(screen.getByRole("button", { name: "Start tracking job" }));

    expect(await screen.findByText("The outcome could not be confirmed. Check Servers before retrying.")).toBeInTheDocument();
    expect(container).not.toHaveTextContent("root-secret");
    expect(container.querySelectorAll("[data-agent-job-notice]")).toHaveLength(0);
  });

  it.each([
    ["en", "Agent operations", "Installing agent"],
    ["es", "Operaciones del agente", "Instalando el agente"],
    ["pt", "Operações do agente", "Instalando o agente"],
  ])("localizes active job labels in %s", async (locale, region, operation) => {
    setLocale(locale);
    renderJobShell(json({ ...queuedInstall, status: "running", stage: "validating" }));

    fireEvent.click(screen.getByRole("button", { name: "Start tracking job" }));

    expect(await screen.findByRole("region", { name: region })).toBeInTheDocument();
    expect(screen.getByText(operation)).toBeInTheDocument();
  });
});
