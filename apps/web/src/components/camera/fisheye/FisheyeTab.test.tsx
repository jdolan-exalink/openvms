import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fisheyeConfigStorageKey } from "@/lib/fisheye/storage";
import { exportPreferenceKey, FisheyeTab } from "./FisheyeTab";

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
    expect(screen.getByText(/Las exportaciones siguen usando el original/)).toBeInTheDocument();
    expect(screen.getByTestId("fisheye-section")).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("reports the dewarped view as off by default", () => {
    render(<FisheyeTab cameraId="c2" cameraName="domo" canManage={false} />);
    expect(screen.getByText("Desactivada")).toBeInTheDocument();
  });

  it("stores the export preference per camera next to the other fisheye settings", () => {
    render(<FisheyeTab cameraId="c3" cameraName="domo" canManage />);
    const select = screen.getByLabelText("Exportar");
    expect(select).toHaveValue("original");
    fireEvent.change(select, { target: { value: "dewarp" } });
    expect(localStorage.getItem(exportPreferenceKey("c3"))).toBe("dewarp");
    expect(screen.getByText(/Las exportaciones siguen usando el original/)).toBeInTheDocument();
  });

  it("restores the stored export preference and locks it without manage permission", () => {
    localStorage.setItem(exportPreferenceKey("c4"), "dewarp");
    render(<FisheyeTab cameraId="c4" cameraName="domo" canManage={false} />);
    expect(screen.getByLabelText("Exportar")).toHaveValue("dewarp");
    expect(screen.getByLabelText("Exportar")).toBeDisabled();
  });
});
