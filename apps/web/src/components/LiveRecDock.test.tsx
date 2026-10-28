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
    subscribePosition: (listener: () => void) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  } as unknown as RecTransport;
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
  const dock = (transport: RecTransport, now = 1775600500) =>
    renderWithClient(<LiveRecDock transport={transport} cameras={mockCameras} events={[]} now={now} />);

  it("frames the clip controls as Exportación, with Inicio and Fin that say what they do", () => {
    const { transport, setPos } = createTransport(1775600100);
    dock(transport);

    expect(screen.getByText("Exportación")).toBeInTheDocument();
    const start = screen.getByRole("button", { name: "Marcar inicio" });
    const end = screen.getByRole("button", { name: "Marcar fin" });
    expect(start).toHaveAttribute("title", expect.stringContaining("Marcar inicio"));
    expect(end).toHaveAttribute("title", expect.stringContaining("Marcar fin"));
    expect(screen.getByText(/tocá Inicio/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Exportar/ })).not.toBeInTheDocument();

    fireEvent.click(start);
    expect(start).toHaveTextContent(/Inicio\d\d:\d\d:\d\d/);
    expect(screen.getByText(/tocá Fin/)).toBeInTheDocument();

    setPos(1775600200);
    fireEvent.click(end);
    expect(end).toHaveTextContent(/Fin\d\d:\d\d:\d\d/);
    expect(screen.getByTestId("export-duration")).toHaveTextContent("1m 40s");
    expect(screen.getByRole("button", { name: /Exportar/ })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Limpiar selección/ }));
    expect(start).not.toHaveTextContent(/\d\d:\d\d:\d\d/);
    expect(screen.queryByRole("button", { name: /Exportar/ })).not.toBeInTheDocument();
  });

  it("handles keyboard shortcuts I and O to mark the range", () => {
    const { transport, setPos } = createTransport(1775600100);
    dock(transport);

    fireEvent.keyDown(window, { key: "i" });
    expect(screen.getByRole("button", { name: "Marcar inicio" })).toHaveTextContent(/\d\d:\d\d:\d\d/);

    setPos(1775600300);
    fireEvent.keyDown(window, { key: "o" });
    expect(screen.getByRole("button", { name: /Exportar/ })).toBeInTheDocument();

    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("button", { name: /Exportar/ })).not.toBeInTheDocument();
  });

  it("takes Ahora to today, fifteen minutes before the current time", () => {
    const { transport } = createTransport(1775500000);
    dock(transport, 1775600500);
    fireEvent.click(screen.getByRole("button", { name: "Ahora" }));
    expect(transport.seek).toHaveBeenCalledWith(1775600500 - 15 * 60);
  });

  it("puts the day picker right after Ahora", () => {
    const { transport } = createTransport();
    dock(transport);
    const ahora = screen.getByRole("button", { name: "Ahora" });
    const picker = screen.getByRole("button", { name: /Elegir día/ });
    expect(ahora.nextElementSibling).toBe(picker.parentElement);
  });

  it("explains every colour of the timeline", () => {
    const { transport } = createTransport();
    dock(transport);
    const legend = screen.getByRole("list", { name: "Referencias de la línea de tiempo" });
    for (const name of ["Grabado", "Grabación continua", "Persona", "Vehículo", "Patente", "Animal", "Movimiento y otros"]) {
      expect(legend).toHaveTextContent(name);
    }
  });
});
