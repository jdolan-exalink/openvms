import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LiveVisionTile } from "./LiveVisionTile";
import { api } from "@/api/client";

vi.mock("@/api/client", () => ({
  api: {
    GET: vi.fn(),
  },
}));

vi.mock("@/components/MsePlayer", () => ({
  MsePlayer: ({ cameraId }: { cameraId: string }) => (
    <div data-testid="mse-player" data-camera-id={cameraId}>
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

function renderWithQuery(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>);
}

describe("LiveVisionTile", () => {
  it("renders radar scanner state when no cameras exist", async () => {
    vi.mocked(api.GET).mockResolvedValueOnce({
      data: { items: [], total: 0 },
      response: new Response(),
    } as unknown as Awaited<ReturnType<typeof api.GET>>);

    renderWithQuery(<LiveVisionTile vision={{ id: "visionai:general", name: "Vision AI" }} />);

    expect(screen.getByText("Vision AI")).toBeInTheDocument();
    expect(screen.getByText(/Buscando actividad inteligente/i)).toBeInTheDocument();
  });

  it("renders active detection and MsePlayer when cameras exist", async () => {
    vi.mocked(api.GET).mockResolvedValueOnce({
      data: {
        items: [
          { id: "cam-1", display_name: "Acceso Principal", status: "online" },
          { id: "cam-2", display_name: "Portón Norte", status: "online" },
        ],
        total: 2,
      },
      response: new Response(),
    } as unknown as Awaited<ReturnType<typeof api.GET>>);

    renderWithQuery(<LiveVisionTile vision={{ id: "visionai:personas", name: "Vision AI - Personas" }} />);

    expect(await screen.findByTestId("mse-player")).toBeInTheDocument();
    expect(screen.getByText("Vision AI - Personas")).toBeInTheDocument();
    expect(screen.getByText("PERSONA")).toBeInTheDocument();
    expect(screen.getByText("Acceso Principal")).toBeInTheDocument();
  });

  it("renders LPR plate and watchlist indicator for LPR preset", async () => {
    vi.mocked(api.GET).mockResolvedValueOnce({
      data: {
        items: [
          { id: "cam-1", display_name: "Acceso Principal", status: "online" },
          { id: "cam-2", display_name: "LPR Portón", status: "online" },
        ],
        total: 2,
      },
      response: new Response(),
    } as unknown as Awaited<ReturnType<typeof api.GET>>);

    renderWithQuery(<LiveVisionTile vision={{ id: "visionai:lpr", name: "Vision AI - LPR" }} />);

    expect(await screen.findByText("AB123CD")).toBeInTheDocument();
    expect(screen.getByText(/Watchlist match/i)).toBeInTheDocument();
  });

  it("toggles pin camera button", async () => {
    vi.mocked(api.GET).mockResolvedValueOnce({
      data: {
        items: [{ id: "cam-1", display_name: "Acceso Principal", status: "online" }],
        total: 1,
      },
      response: new Response(),
    } as unknown as Awaited<ReturnType<typeof api.GET>>);

    renderWithQuery(<LiveVisionTile vision={{ id: "visionai:general", name: "Vision AI" }} />);

    const pinButton = await screen.findByRole("button", { name: /Fijar cámara/i });
    fireEvent.click(pinButton);

    expect(await screen.findByRole("button", { name: /Desfijar cámara/i })).toBeInTheDocument();
  });
});
