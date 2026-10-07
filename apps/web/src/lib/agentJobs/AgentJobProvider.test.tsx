import { useState } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { AgentJobProvider, useAgentJobs } from "./AgentJobProvider";

const acceptedJob = {
  id: "job-1",
  status: "queued" as const,
  stage: "validating" as const,
  message: "Install queued",
};

function RouteHarness() {
  const [route, setRoute] = useState("servers");
  const { registerJob } = useAgentJobs();

  if (route !== "servers") return <p>Other route mounted</p>;
  return <button onClick={() => {
    registerJob({ kind: "install", serverId: "server-1", serverName: "North lab", job: acceptedJob });
    setRoute("other");
  }}>Accept install and navigate</button>;
}

function renderProvider() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <AgentJobProvider>
        <RouteHarness />
      </AgentJobProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("AgentJobProvider", () => {
  it("continues polling an accepted job after the Servers route unmounts", async () => {
    let polls = 0;
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "GET /api/v1/servers/server-1/agent/install/job-1": () => {
        polls++;
        return json({ ...acceptedJob, status: "running", stage: "transferring", message: "Transferring agent files over SFTP" });
      },
    })));
    renderProvider();

    fireEvent.click(screen.getByRole("button", { name: "Accept install and navigate" }));
    expect(await screen.findByText("Other route mounted")).toBeInTheDocument();
    await waitFor(() => expect(polls).toBeGreaterThan(0));
  });

  it("does not create a second poller when the same accepted job is registered twice", async () => {
    let polls = 0;
    function DuplicateRegistration() {
      const { registerJob } = useAgentJobs();
      return <button onClick={() => {
        const accepted = { kind: "update" as const, serverId: "server-1", serverName: "North lab", job: acceptedJob };
        registerJob(accepted);
        registerJob(accepted);
      }}>Register twice</button>;
    }
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "GET /api/v1/servers/server-1/agent/update-ssh/job-1": () => {
        polls++;
        return json({ ...acceptedJob, status: "running", stage: "transferring" });
      },
    })));
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={queryClient}>
        <AgentJobProvider><DuplicateRegistration /></AgentJobProvider>
      </QueryClientProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Register twice" }));
    await waitFor(() => expect(polls).toBeGreaterThan(0));
    expect(polls).toBe(1);
  });

  it("keeps equal job IDs isolated by operation kind and server", async () => {
    const requested: string[] = [];
    function RegisterCollidingJobs() {
      const { registerJob, jobs } = useAgentJobs();
      return <>
        <button onClick={() => {
          registerJob({ kind: "install", serverId: "server-1", serverName: "North lab", job: acceptedJob });
          registerJob({ kind: "update", serverId: "server-2", serverName: "South lab", job: acceptedJob });
        }}>Register colliding jobs</button>
        <output>{jobs.map((entry) => `${entry.kind}:${entry.serverId}:${entry.job.id}`).join(",")}</output>
      </>;
    }
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "GET /api/v1/servers/server-1/agent/install/job-1": () => {
        requested.push("install:server-1");
        return json({ ...acceptedJob, status: "running", stage: "transferring" });
      },
      "GET /api/v1/servers/server-2/agent/update-ssh/job-1": () => {
        requested.push("update:server-2");
        return json({ ...acceptedJob, status: "running", stage: "transferring" });
      },
    })));
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={queryClient}>
        <AgentJobProvider><RegisterCollidingJobs /></AgentJobProvider>
      </QueryClientProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Register colliding jobs" }));
    await waitFor(() => expect(requested).toHaveLength(2));
    expect(screen.getByText("install:server-1:job-1,update:server-2:job-1")).toBeInTheDocument();
  });
});
