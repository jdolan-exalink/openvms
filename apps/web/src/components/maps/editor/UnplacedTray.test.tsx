import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { DRAG_MIME, UnplacedTray } from "./UnplacedTray";

const cameras = [
  { id: "cu1", name: "Recepción", status: "online" },
  { id: "cu2", name: "Parqueadero", status: "offline" },
];

describe("UnplacedTray", () => {
  it("lists the site cameras that have no placement", () => {
    render(<UnplacedTray cameras={cameras} onArm={() => {}} onPlaceAll={() => {}} />);
    expect(screen.getByRole("region", { name: "Sin ubicar" })).toHaveTextContent("Sin ubicar (2)");
    expect(screen.getByRole("button", { name: "Recepción" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Parqueadero" })).toBeInTheDocument();
  });

  it("arms a camera so the next map click places it", () => {
    const onArm = vi.fn();
    render(<UnplacedTray cameras={cameras} onArm={onArm} onPlaceAll={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Recepción" }));
    expect(onArm).toHaveBeenCalledWith("cu1");
  });

  it("marks the armed camera so the operator knows what the next click drops", () => {
    render(<UnplacedTray cameras={cameras} armedId="cu2" onArm={() => {}} onPlaceAll={() => {}} />);
    expect(screen.getByRole("button", { name: "Parqueadero" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Recepción" })).toHaveAttribute("aria-pressed", "false");
  });

  it("carries the camera id through a drag", () => {
    const setData = vi.fn();
    render(<UnplacedTray cameras={cameras} onArm={() => {}} onPlaceAll={() => {}} />);
    fireEvent.dragStart(screen.getByRole("button", { name: "Recepción" }), {
      dataTransfer: { setData },
    });
    expect(setData).toHaveBeenCalledWith(DRAG_MIME, "cu1");
  });

  it("offers the bulk placement at the site centre", () => {
    const onPlaceAll = vi.fn();
    render(<UnplacedTray cameras={cameras} onArm={() => {}} onPlaceAll={onPlaceAll} />);
    fireEvent.click(screen.getByRole("button", { name: "Ubicar todas en el centro del sitio" }));
    expect(onPlaceAll).toHaveBeenCalledTimes(1);
  });

  it("says so when nothing is left to place", () => {
    render(<UnplacedTray cameras={[]} onArm={() => {}} onPlaceAll={() => {}} />);
    expect(screen.getByText(/Todas las cámaras del sitio están ubicadas/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Ubicar todas en el centro del sitio" })).not.toBeInTheDocument();
  });
});
