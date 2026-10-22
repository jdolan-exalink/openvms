import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LiveVisionTile } from "./LiveVisionTile";
import { api } from "@/api/client";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: {
    GET: vi.fn(),
  },
}));

vi.mock("@/components/MsePlayer", () => ({
  MsePlayer: ({ cameraId, quality, className }: { cameraId: string; quality?: string; className?: string }) => (
    <div data-testid="mse-player" data-camera-id={cameraId} data-quality={quality} data-class={className}>
      Player {cameraId}
    </div>
  ),
}));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, ...props }: { children: React.ReactNode; to: string }) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
}));

type Slot = Record<string, unknown>;
let slotsBody: { slots: Slot[] };
let readsBody: Slot[];

function filled(cam: string, extra: Slot = {}): Slot {
  return { slot: 0, camera_id: cam, pinned: false, camera_name: `Cam ${cam}`, object: "person", confidence: 0.96, zone: "Perímetro", ...extra };
}

function renderWithQuery(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>);
  return { queryClient, ...view };
}

const slotQueries = () =>
  (vi.mocked(api.GET).mock.calls as unknown as [string, { params: { query: { source: string; layout: string } } }][]).filter(
    ([path]) => path === "/api/v1/vision-ai/slots",
  );

beforeEach(() => {
  vi.mocked(api.GET).mockReset();
  slotsBody = { slots: [{ slot: 0, camera_id: null, pinned: false }] };
  readsBody = [];
  vi.mocked(api.GET).mockImplementation((async (path: string) => {
    if (path === "/api/v1/vision-ai/slots") {
      return { data: { source: "general", layout: "1", generated_at: "2026-10-15T10:00:00Z", ...slotsBody }, response: new Response() };
    }
    if (path === "/api/v1/lpr/reads") return { data: { items: readsBody }, response: new Response() };
    return { data: undefined, error: { code: "not_found", message: "nope" }, response: new Response(null, { status: 404 }) };
  }) as unknown as typeof api.GET);
});

describe("LiveVisionTile", () => {
  it("shows the plate list overlay on a slot that carries a plate, and not on other slots", async () => {
    slotsBody = { slots: [{ ...filled("cam-1"), plate: "AB123CD" }] };
    readsBody = [{ id: "r1", camera_id: "cam-1", camera_name: "Cam cam-1", plate: "AB123CD", plate_normalized: "AB123CD", label: "car", zones: [], seen_at: "2026-10-15T10:00:00Z" }];
    const { unmount } = renderWithQuery(<LiveVisionTile vision={{ id: "visionai:lpr", name: "Vision AI - LPR", layout: "lpr" }} />);
    expect(await screen.findByTestId("detection-overlay")).toBeInTheDocument();
    unmount();

    slotsBody = { slots: [filled("cam-2")] };
    renderWithQuery(<LiveVisionTile vision={{ id: "visionai:general", name: "Vision AI" }} />);
    await screen.findByTestId("mse-player");
    expect(screen.queryByTestId("detection-overlay")).not.toBeInTheDocument();
  });

  it.each([
    ["visionai:lpr-channel", "lpr"],
    ["visionai:faces-channel", "faces"],
  ])("renders %s as a detection channel instead of a camera slot", async (id, kind) => {
    renderWithQuery(<LiveVisionTile vision={{ id, name: "Canal" }} />);
    expect((await screen.findByTestId("detection-channel")).getAttribute("data-channel")).toBe(kind);
    expect(slotQueries()).toHaveLength(0);
    expect(screen.queryByTestId("mse-player")).not.toBeInTheDocument();
  });

  it("renders the radar state when the slot has no camera", async () => {
    renderWithQuery(<LiveVisionTile vision={{ id: "visionai:general", name: "Vision AI" }} />);

    expect(screen.getByText("Vision AI")).toBeInTheDocument();
    expect(screen.getByText(/Buscando actividad inteligente/i)).toBeInTheDocument();
    await waitFor(() => expect(slotQueries()).toHaveLength(1));
    expect(screen.queryByTestId("mse-player")).not.toBeInTheDocument();
  });

  it("derives the source from the vision id and asks for a single slot", async () => {
    renderWithQuery(<LiveVisionTile vision={{ id: "visionai:personas", name: "Vision AI - Personas" }} />);
    await waitFor(() => expect(slotQueries()).toHaveLength(1));
    expect(slotQueries()[0]![1].params.query).toEqual({ source: "personas", layout: "1" });
  });

  it("falls back to the general source for an unknown id", async () => {
    renderWithQuery(<LiveVisionTile vision={{ id: "visionai:desconocido", name: "Vision AI" }} />);
    await waitFor(() => expect(slotQueries()).toHaveLength(1));
    expect(slotQueries()[0]![1].params.query.source).toBe("general");
  });

  it("shows the slot camera in a full-size player with the slot HUD", async () => {
    slotsBody = { slots: [filled("cam-1", { alarm: "critical", camera_name: "Acceso Principal" })] };
    renderWithQuery(<LiveVisionTile vision={{ id: "visionai:personas", name: "Vision AI - Personas" }} />);

    const player = await screen.findByTestId("mse-player");
    expect(player).toHaveAttribute("data-camera-id", "cam-1");
    expect(player.getAttribute("data-class")).toContain("size-full");
    expect(player).toHaveAttribute("data-quality", "sub");
    expect(screen.getByText("Vision AI - Personas")).toBeInTheDocument();
    expect(screen.getByText("Acceso Principal")).toBeInTheDocument();
    expect(screen.getByText("person")).toBeInTheDocument();
    expect(screen.getByText(/96%/)).toBeInTheDocument();
  });

  it("uses the main stream when focused", async () => {
    slotsBody = { slots: [filled("cam-1")] };
    renderWithQuery(<LiveVisionTile vision={{ id: "visionai:general", name: "Vision AI" }} isFocused />);
    expect(await screen.findByTestId("mse-player")).toHaveAttribute("data-quality", "main");
  });

  it("renders the LPR plate banner only when the slot carries a plate", async () => {
    slotsBody = { slots: [filled("cam-2", { object: "car", plate: "AB123CD" })] };
    renderWithQuery(<LiveVisionTile vision={{ id: "visionai:lpr", name: "Vision AI - LPR" }} />);
    expect(await screen.findByText("AB123CD")).toBeInTheDocument();
    expect(screen.queryByText(/Watchlist/i)).not.toBeInTheDocument();
  });

  it("follows the ranked camera when it changes", async () => {
    slotsBody = { slots: [filled("cam-1")] };
    const { queryClient } = renderWithQuery(<LiveVisionTile vision={{ id: "visionai:general", name: "Vision AI" }} />);
    expect(await screen.findByTestId("mse-player")).toHaveAttribute("data-camera-id", "cam-1");

    slotsBody = { slots: [filled("cam-9")] };
    await act(() => queryClient.invalidateQueries({ queryKey: ["vision-ai"] }));
    await waitFor(() => expect(screen.getByTestId("mse-player")).toHaveAttribute("data-camera-id", "cam-9"));
  });

  it("pin freezes the current camera across ranking changes and unpin resumes", async () => {
    slotsBody = { slots: [filled("cam-1")] };
    const { queryClient } = renderWithQuery(<LiveVisionTile vision={{ id: "visionai:general", name: "Vision AI" }} />);
    await screen.findByTestId("mse-player");

    fireEvent.click(screen.getByRole("button", { name: /Fijar cámara/i }));
    expect(await screen.findByRole("button", { name: /Desfijar cámara/i })).toBeInTheDocument();

    slotsBody = { slots: [filled("cam-9")] };
    await act(() => queryClient.invalidateQueries({ queryKey: ["vision-ai"] }));
    await waitFor(() => expect(slotQueries().length).toBeGreaterThan(1));
    expect(screen.getByTestId("mse-player")).toHaveAttribute("data-camera-id", "cam-1");

    fireEvent.click(screen.getByRole("button", { name: /Desfijar cámara/i }));
    await waitFor(() => expect(screen.getByTestId("mse-player")).toHaveAttribute("data-camera-id", "cam-9"));
  });

  it("drops the pin when the vision source changes", async () => {
    slotsBody = { slots: [filled("cam-1")] };
    const view = renderWithQuery(<LiveVisionTile vision={{ id: "visionai:general", name: "Vision AI" }} />);
    await screen.findByTestId("mse-player");
    fireEvent.click(screen.getByRole("button", { name: /Fijar cámara/i }));
    expect(await screen.findByRole("button", { name: /Desfijar cámara/i })).toBeInTheDocument();

    slotsBody = { slots: [filled("cam-7")] };
    view.rerender(
      <QueryClientProvider client={view.queryClient}>
        <LiveVisionTile vision={{ id: "visionai:personas", name: "Personas" }} />
      </QueryClientProvider>,
    );
    await waitFor(() => expect(screen.getByTestId("mse-player")).toHaveAttribute("data-camera-id", "cam-7"));
    expect(screen.getByRole("button", { name: /Fijar cámara/i })).toBeInTheDocument();
  });

  it("does not restore an old pin when switching A -> B -> A", async () => {
    const tile = (id: string, client: QueryClient) => (
      <QueryClientProvider client={client}>
        <LiveVisionTile vision={{ id, name: id }} />
      </QueryClientProvider>
    );
    slotsBody = { slots: [filled("cam-1")] };
    const view = renderWithQuery(<LiveVisionTile vision={{ id: "visionai:general", name: "A" }} />);
    await screen.findByTestId("mse-player");
    fireEvent.click(screen.getByRole("button", { name: /Fijar cámara/i }));
    expect(await screen.findByRole("button", { name: /Desfijar cámara/i })).toBeInTheDocument();

    slotsBody = { slots: [filled("cam-7")] };
    view.rerender(tile("visionai:personas", view.queryClient));
    await waitFor(() => expect(screen.getByTestId("mse-player")).toHaveAttribute("data-camera-id", "cam-7"));

    slotsBody = { slots: [filled("cam-5")] };
    view.rerender(tile("visionai:general", view.queryClient));
    await waitFor(() => expect(screen.getByTestId("mse-player")).toHaveAttribute("data-camera-id", "cam-5"));
    expect(screen.getByRole("button", { name: /Fijar cámara/i })).toBeInTheDocument();
  });
  it("shows the object icon in the detection badge", async () => {
    slotsBody = { slots: [filled("cam-1", { object: "motorcycle" })] };
    renderWithQuery(<LiveVisionTile vision={{ id: "visionai:general", name: "Vision AI" }} />);
    await screen.findByTestId("mse-player");
    const badge = screen.getByText("motorcycle").parentElement!;
    expect(badge.querySelector("svg")).toHaveClass("lucide-bike");
    expect(badge.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
  });
});
