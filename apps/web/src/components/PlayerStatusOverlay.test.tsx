import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { SessionSnapshot } from "@/lib/live/PlayerSession";
import { streamError } from "@/lib/live/streamErrors";
import { PlayerStatusOverlay } from "./PlayerStatusOverlay";

const snap = (patch: Partial<SessionSnapshot>): SessionSnapshot => ({ state: "CONNECTING", message: "", error: null, poster: null, ...patch });

describe("PlayerStatusOverlay", () => {
  it("shows the camera snapshot (never a bare black tile) while reconnecting after the camera went offline", () => {
    const error = streamError("camera_offline");
    const { container } = render(<PlayerStatusOverlay cameraId="cam-1" snapshot={snap({ state: "RECONNECTING", message: error.label, error })} />);
    expect(container.querySelector("img")?.getAttribute("src")).toContain("/media/v1/cameras/cam-1/snapshot.jpg");
    expect(screen.getByText("Cámara sin conexión")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reintentar" })).toBeNull();
  });

  it("prefers the in-memory last frame over the cold snapshot", () => {
    const { container } = render(<PlayerStatusOverlay cameraId="cam-1" snapshot={snap({ state: "SUSPENDED", poster: "blob:frame" })} />);
    expect(container.querySelector("img")?.getAttribute("src")).toBe("blob:frame");
    expect(screen.queryByRole("progressbar")).toBeNull();
  });

  it("offers Reintentar for stopped errors", () => {
    const retry = vi.fn();
    const error = streamError("unauthorized");
    render(<PlayerStatusOverlay cameraId="cam-1" snapshot={snap({ state: "ERROR", message: error.label, error })} onRetry={retry} />);
    expect(screen.getByText("Sesión expirada")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Reintentar" }));
    expect(retry).toHaveBeenCalledTimes(1);
  });
});
