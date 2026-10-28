import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { RecordingPlayer } from "./RecordingPlayer";

vi.mock("@/components/HlsPlayer", () => ({ HlsPlayer: () => <div data-testid="hls" /> }));
vi.mock("@/components/ReplayPlayer", () => ({ ReplayPlayer: (p: { quality?: string }) => <div data-testid="replay" data-quality={p.quality} /> }));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const wrap = (ui: React.ReactNode) =>
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{ui}</QueryClientProvider>);

const stubKinds = (kind: string | null) =>
  vi.stubGlobal("fetch", vi.fn(stubApi({
    "/api/v1/cameras/c1": () => json({ id: "c1", server_id: "s1" }),
    "/api/v1/servers": () => json({ items: [{ id: "s1", kind: kind ?? "frigate" }, { id: "s2", kind: "provision_nvr" }] }),
  })));

describe("RecordingPlayer", () => {
  it("plays a Frigate camera through HLS", async () => {
    stubKinds("frigate");
    wrap(<RecordingPlayer cameraId="c1" start={1} end={2} />);
    expect(await screen.findByTestId("hls")).toBeInTheDocument();
    expect(screen.queryByTestId("replay")).not.toBeInTheDocument();
  });

  it("plays a recorder camera through its replay, passing the quality along", async () => {
    stubKinds("provision_nvr");
    wrap(<RecordingPlayer cameraId="c1" start={1} end={2} quality="sub" />);
    const replay = await screen.findByTestId("replay");
    expect(replay).toHaveAttribute("data-quality", "sub");
    expect(screen.queryByTestId("hls")).not.toBeInTheDocument();
  });

  it("shows neither player while it does not know the camera's server, so a recorder never hits the HLS route", async () => {
    stubKinds("provision_nvr");
    wrap(<RecordingPlayer cameraId="c1" start={1} end={2} ariaLabel="grabación" />);
    expect(screen.getByLabelText("grabación")).toHaveAttribute("aria-busy", "true");
    expect(screen.queryByTestId("hls")).not.toBeInTheDocument();
    expect(screen.queryByTestId("replay")).not.toBeInTheDocument();
    await screen.findByTestId("replay");
  });

  it("falls back to HLS, which reports the real error, when the camera cannot be read", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi({ "/api/v1/servers": () => json({ items: [] }) })));
    wrap(<RecordingPlayer cameraId="c1" start={1} end={2} />);
    expect(await screen.findByTestId("hls")).toBeInTheDocument();
  });
});
