import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LiveRecDock } from "./LiveRecDock";
import type { RecTransport } from "@/lib/useRecPlayback";

function createTransport(initialPos = 1775600100) {
  let pos = initialPos;
  const listeners = new Set<() => void>();
  const transport: RecTransport = {
    seek: vi.fn((next) => {
      pos = next;
      listeners.forEach((l) => l());
    }),
    play: vi.fn(),
    pause: vi.fn(),
    playing: false,
    speed: 1,
    setSpeed: vi.fn(),
    day: 1775600000,
    getPosition: () => pos,
    subscribePosition: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
  return {
    transport,
    setPos: (next: number) => {
      act(() => {
        pos = next;
        listeners.forEach((l) => l());
      });
    },
  };
}

const mockCameras = [
  { id: "c1", name: "Camera 1", spans: [{ start: 1775600000, end: 1775600500, type: "motion" as const }] },
];

function renderWithClient(ui: React.ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

describe("LiveRecDock", () => {
  it("renders IN and OUT buttons and marks range", () => {
    const { transport, setPos } = createTransport(1775600100);
    renderWithClient(
      <LiveRecDock
        transport={transport}
        cameras={mockCameras}
        events={[]}
        now={1775600500}
      />
    );

    const inBtn = screen.getByRole("button", { name: /Marcar punto inicial IN/i });
    const outBtn = screen.getByRole("button", { name: /Marcar punto final OUT/i });
    expect(inBtn).toBeInTheDocument();
    expect(outBtn).toBeInTheDocument();

    // Mark IN at 1775600100
    fireEvent.click(inBtn);
    expect(screen.getByText(/IN:/i)).toBeInTheDocument();

    // Advance transport position to 1775600200 and mark OUT
    setPos(1775600200);
    fireEvent.click(outBtn);
    expect(screen.getByText(/OUT:/i)).toBeInTheDocument();

    // Export button appears
    const exportBtn = screen.getByRole("button", { name: /Exportar/i });
    expect(exportBtn).toBeInTheDocument();

    // Clear selection
    const clearBtn = screen.getByRole("button", { name: /Limpiar selección/i });
    fireEvent.click(clearBtn);
    expect(screen.queryByText(/IN:/i)).not.toBeInTheDocument();
  });

  it("handles keyboard shortcuts I and O to mark range", () => {
    const { transport, setPos } = createTransport(1775600100);
    renderWithClient(
      <LiveRecDock
        transport={transport}
        cameras={mockCameras}
        events={[]}
        now={1775600500}
      />
    );

    fireEvent.keyDown(window, { key: "i" });
    expect(screen.getByText(/IN:/i)).toBeInTheDocument();

    setPos(1775600300);
    fireEvent.keyDown(window, { key: "o" });
    expect(screen.getByText(/OUT:/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Exportar/i })).toBeInTheDocument();

    // Press Escape to clear
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByText(/IN:/i)).not.toBeInTheDocument();
  });
});
