import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fisheyeConfigStorageKey } from "@/lib/fisheye/storage";
import { FisheyeTab } from "./FisheyeTab";

vi.mock("@/components/fisheye/FisheyeSection", () => ({ FisheyeSection: () => <div data-testid="fisheye-section" /> }));

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe("FisheyeTab", () => {
  it("separates the original stream and recording from the dewarped view, and never calls the API", () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    localStorage.setItem(fisheyeConfigStorageKey("c1"), JSON.stringify({ enabled: true, defaultMode: "3_fixed_120" }));
    render(<FisheyeTab cameraId="c1" cameraName="domo" canManage />);
    expect(screen.getByText("Stream original")).toBeInTheDocument();
    expect(screen.getByText("Grabación original")).toBeInTheDocument();
    expect(screen.getByText(/Activa · 3 vistas fijas de 120°/)).toBeInTheDocument();
    expect(screen.getByText(/nunca modifica el stream ni la grabación/)).toBeInTheDocument();
    expect(screen.getByText(/todavía no hay una preferencia guardada por cámara/)).toBeInTheDocument();
    expect(screen.getByTestId("fisheye-section")).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("reports the dewarped view as off by default", () => {
    render(<FisheyeTab cameraId="c2" cameraName="domo" canManage={false} />);
    expect(screen.getByText("Desactivada")).toBeInTheDocument();
  });
});
