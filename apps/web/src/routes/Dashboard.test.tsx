import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Dashboard } from "./Dashboard";

afterEach(() => vi.unstubAllGlobals());

describe("Dashboard", () => {
  it("shows a failing dependency when readiness returns 503", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/health/ready": () =>
            json(
              {
                status: "degraded",
                checks: [
                  { name: "postgres", status: "ok", latency_ms: 2 },
                  { name: "nats", status: "error", latency_ms: 2000, error: "connection refused" },
                ],
              },
              503,
            ),
          "/api/v1/system/info": () =>
            json({ name: "openvms-api", version: "v0.1.0", commit: "abc", build_time: "", go_version: "go1.26", schema_version: 2 }),
          "/api/v1/sites": () => json({ items: [] }),
          "/api/v1/servers": () => json({ items: [{ status: "online" }, { status: "offline" }] }),
          "/api/v1/cameras": () => json({ items: [] }),
        }),
      ),
    );

    renderPage(Dashboard);

    expect(await screen.findByText("Degradado")).toBeInTheDocument();
    expect(screen.getByText("NATS JetStream")).toBeInTheDocument();
    expect(screen.getByText("connection refused")).toBeInTheDocument();
    expect(await screen.findByText("v2")).toBeInTheDocument();
    expect(await screen.findByText("1 en línea")).toBeInTheDocument();
  });
});
