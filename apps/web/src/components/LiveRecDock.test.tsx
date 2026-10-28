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

  it("frames the clip controls as Exportación and walks through Inicio, Fin and Exportar", () => {
    const { transport, setPos } = createTransport(1775600100);
    dock(transport);

    expect(screen.getByText("Exportación")).toBeInTheDocument();
    const start = () => screen.getByRole("button", { name: /inicio$/i });
    const end = () => screen.getByRole("button", { name: /fin$/i });
    const exportBtn = () => screen.getByRole("button", { name: "Exportar" });

    // Nothing marked: Fin and Exportar are locked.
    expect(start()).toBeEnabled();
    expect(end()).toBeDisabled();
    expect(end()).toHaveAttribute("title", expect.stringContaining("marcá el inicio"));
    expect(exportBtn()).toBeDisabled();
    expect(screen.getByRole("status")).toHaveTextContent(/Tocá Inicio/);

    // First press: Inicio is being placed and follows the cursor; Fin stays locked.
    fireEvent.click(start());
    expect(start()).toHaveAccessibleName("Fijar inicio");
    expect(start()).toHaveAttribute("aria-pressed", "true");
    expect(end()).toBeDisabled();
    expect(end()).toHaveAttribute("title", expect.stringContaining("fijá el inicio"));
    setPos(1775600150);
    fireEvent.click(start());
    expect(start()).toHaveAccessibleName("Mover inicio");
    expect(start()).toHaveAttribute("aria-pressed", "false");
    expect(start()).toHaveTextContent(/Inicio\d\d:\d\d:\d\d/);

    // Inicio is fixed: Fin opens. Placing Fin locks Inicio and Exportar.
    expect(end()).toBeEnabled();
    fireEvent.click(end());
    expect(end()).toHaveAccessibleName("Fijar fin");
    expect(start()).toBeDisabled();
    expect(exportBtn()).toBeDisabled();
    setPos(1775600250);
    fireEvent.click(end());
    expect(end()).toHaveAccessibleName("Mover fin");

    // Both fixed: only now Exportar is enabled.
    expect(screen.getByTestId("export-duration")).toHaveTextContent("1m 40s");
    expect(exportBtn()).toBeEnabled();
    expect(start()).toBeEnabled();

    fireEvent.click(screen.getByRole("button", { name: /Limpiar selección/ }));
    expect(end()).toBeDisabled();
    expect(exportBtn()).toBeDisabled();
  });

  it("does not fix an end that is not after the start", () => {
    const { transport, setPos } = createTransport(1775600200);
    dock(transport);
    fireEvent.click(screen.getByRole("button", { name: "Marcar inicio" }));
    fireEvent.click(screen.getByRole("button", { name: "Fijar inicio" }));
    fireEvent.click(screen.getByRole("button", { name: "Marcar fin" }));
    setPos(1775600100);
    fireEvent.click(screen.getByRole("button", { name: "Fijar fin" }));
    expect(screen.getByRole("button", { name: "Fijar fin" })).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent(/posterior al inicio/);
    expect(screen.getByRole("button", { name: "Exportar" })).toBeDisabled();
  });

  it("follows the same two presses with keys I and O, and Escape stops placing before it clears", () => {
    const { transport, setPos } = createTransport(1775600100);
    dock(transport);

    fireEvent.keyDown(window, { key: "o" }); // locked: no start yet
    expect(screen.getByRole("button", { name: "Marcar fin" })).toBeDisabled();

    fireEvent.keyDown(window, { key: "i" });
    expect(screen.getByRole("button", { name: "Fijar inicio" })).toBeInTheDocument();
    fireEvent.keyDown(window, { key: "i" });
    expect(screen.getByRole("button", { name: "Mover inicio" })).toBeInTheDocument();

    setPos(1775600300);
    fireEvent.keyDown(window, { key: "o" });
    fireEvent.keyDown(window, { key: "Escape" }); // stops placing the end
    expect(screen.getByRole("button", { name: "Marcar fin" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Mover inicio" })).toBeInTheDocument();

    fireEvent.keyDown(window, { key: "Escape" }); // nothing placing: clears
    expect(screen.getByRole("button", { name: "Marcar inicio" })).toBeInTheDocument();
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
