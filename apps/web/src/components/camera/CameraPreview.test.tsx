import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { CameraPreview } from "./CameraPreview";

vi.mock("@/components/MsePlayer", () => ({
  MsePlayer: ({ quality, muted }: { quality: string; muted: boolean }) => <div data-testid="player" data-quality={quality} data-muted={String(muted)} />,
}));

describe("CameraPreview", () => {
  it("shows a snapshot first and the shared live player after play", () => {
    render(<CameraPreview cameraId="c1" name="acceso" />);
    expect(screen.getByAltText("Vista previa de acceso")).toBeInTheDocument();
    expect(screen.queryByTestId("player")).not.toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("Reproducir"));
    expect(screen.getByTestId("player")).toHaveAttribute("data-muted", "true");
    expect(screen.getByText(/EN VIVO · SUB/)).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("Activar sonido"));
    expect(screen.getByTestId("player")).toHaveAttribute("data-muted", "false");
  });

  it("switches the stream, maximizes and offers the snapshot", () => {
    render(<CameraPreview cameraId="c1" name="acceso" />);
    fireEvent.click(screen.getByLabelText("Reproducir"));
    fireEvent.click(screen.getByRole("button", { name: "Main" }));
    expect(screen.getByTestId("player")).toHaveAttribute("data-quality", "main");
    fireEvent.click(screen.getByLabelText("Maximizar"));
    expect(screen.getByLabelText("Restaurar")).toBeInTheDocument();
    expect(screen.getByLabelText("Capturar imagen")).toHaveAttribute("href", "/media/v1/cameras/c1/snapshot.jpg");
  });
});
