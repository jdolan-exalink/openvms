import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EvidencePlayerModal, type EvidencePlayerJob } from "./EvidencePlayerModal";

const mockJob: EvidencePlayerJob = {
  id: "job-101",
  name: "Export Case #101",
  start_time: "2026-10-12T10:00:00Z",
  end_time: "2026-10-12T10:05:00Z",
  camera_count: 1,
  total_bytes: 5242880,
  items: [
    {
      id: "item-1",
      camera_id: "cam-lobby",
      camera_name: "Lobby Frontal",
      server_name: "hq-server",
      sha256_hash: "a1b2c3d4e5f67890123456789abcdef0",
      total_bytes: 5242880,
      status: "completed",
      video_url: "blob:http://localhost/test-video.mp4",
    },
  ],
};

describe("EvidencePlayerModal Enhancement Integration", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("opens enhancement settings from the promoted icon and changes modes there", () => {
    render(
      <QueryClientProvider client={queryClient}>
        <EvidencePlayerModal job={mockJob} onClose={vi.fn()} />
      </QueryClientProvider>,
    );

    expect(screen.queryByText("AUTO")).not.toBeInTheDocument();
    const enhancementIcon = screen.getByRole("button", { name: /Ajustes de mejora visual/i });
    expect(enhancementIcon.querySelector("svg")).toBeInTheDocument();
    fireEvent.click(enhancementIcon);
    expect(screen.getByRole("dialog", { name: "Panel de mejora visual" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Off" }));
    fireEvent.click(screen.getByRole("button", { name: "Auto" }));
    expect(screen.getByRole("dialog", { name: "Panel de mejora visual" })).toBeInTheDocument();
  });

  it("applies CSS filter to the video element when manual adjustments change", () => {
    render(
      <QueryClientProvider client={queryClient}>
        <EvidencePlayerModal job={mockJob} onClose={vi.fn()} />
      </QueryClientProvider>,
    );

    const video = document.body.querySelector("video") as HTMLVideoElement;
    expect(video).toBeInTheDocument();
    expect(video.getAttribute("crossOrigin")).toBe("anonymous");
    expect(video.style.filter).toBe("");

    // Open options popover
    const optionsBtn = screen.getByRole("button", { name: /Ajustes de mejora visual/i });
    fireEvent.click(optionsBtn);

    // Switch to manual mode
    const manualBtn = screen.getByRole("button", { name: /Manual/i });
    fireEvent.click(manualBtn);

    // Adjust brightness slider to +25
    const brightnessSlider = screen.getByLabelText(/Brillo/i);
    fireEvent.change(brightnessSlider, { target: { value: "25" } });

    // CSS filter should now be applied to the <video> element
    expect(video.style.filter).toContain("brightness(125%)");

    // Adjust contrast slider to +15
    const contrastSlider = screen.getByLabelText(/Contraste/i);
    fireEvent.change(contrastSlider, { target: { value: "15" } });
    expect(video.style.filter).toContain("contrast(115%)");

    // Reset image adjustments
    const resetBtn = screen.getByRole("button", { name: /Restablecer imagen/i });
    fireEvent.click(resetBtn);

    // In auto mode with default adjustments, filter is none/cleared
    expect(video.style.filter).toBe("");
  });

  it("renders 'Mejorar captura' button when paused and opens ForensicComparisonModal on click", async () => {
    render(
      <QueryClientProvider client={queryClient}>
        <EvidencePlayerModal job={mockJob} onClose={vi.fn()} />
      </QueryClientProvider>,
    );

    // Pause playback so 'Mejorar captura' becomes active
    const pauseBtn = screen.getByRole("button", { name: /Pausar/i });
    fireEvent.click(pauseBtn);

    // 'Mejorar captura' button should now be visible on paused tile and transport bar
    const enhanceBtns = screen.getAllByRole("button", { name: /Mejorar captura/i });
    expect(enhanceBtns.length).toBeGreaterThan(0);

    // Click 'Mejorar captura' on first tile
    const [firstEnhanceBtn] = enhanceBtns;
    expect(firstEnhanceBtn).toBeDefined();
    fireEvent.click(firstEnhanceBtn!);

    // Forensic comparison modal should open
    expect(await screen.findByText("Mejora de Detalle Forense")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /descargar original/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /descargar mejorada/i })).toBeInTheDocument();

    // Close modal
    const closeBtn = screen.getByRole("button", { name: /cerrar mejora forense/i });
    fireEvent.click(closeBtn);
    expect(screen.queryByText("Mejora de Detalle Forense")).not.toBeInTheDocument();
  });
});

