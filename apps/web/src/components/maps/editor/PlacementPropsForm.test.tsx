import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { PlacementPropsForm } from "./PlacementPropsForm";
import { DEFAULT_PLACEMENT, type DraftPlacement } from "@/lib/maps/placementDraft";

const draft = (over: Partial<DraftPlacement> = {}): DraftPlacement => ({
  entityId: "c1",
  entityType: "camera",
  siteId: "s",
  lat: -34.6037,
  lng: -58.3816,
  ...DEFAULT_PLACEMENT,
  ...over,
});

describe("PlacementPropsForm", () => {
  it("changes the camera icon type between bullet, dome and ptz", () => {
    const onChange = vi.fn();
    render(<PlacementPropsForm name="Recepción" draft={draft({ cameraType: "fixed" })} onChange={onChange} />);

    expect(screen.getByRole("button", { name: "Bullet" })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(screen.getByRole("button", { name: "Domo" }));
    expect(onChange).toHaveBeenCalledWith({ cameraType: "dome" });
    fireEvent.click(screen.getByRole("button", { name: "PTZ" }));
    expect(onChange).toHaveBeenCalledWith({ cameraType: "ptz" });
  });

  it("shows what is about to be saved, not the stored placement", () => {
    render(<PlacementPropsForm name="Recepción" draft={draft({ lat: -31.42, lng: -64.18, bearingDeg: 45, fovDeg: 90, rangeM: 120 })} onChange={() => {}} />);
    expect(screen.getByRole("region", { name: "Propiedades de la cámara" })).toBeInTheDocument();
    expect(screen.getByText("Recepción")).toBeInTheDocument();
    expect(screen.getByText("-31.42000, -64.18000")).toBeInTheDocument();
    expect(screen.getByLabelText("Rumbo")).toHaveValue(45);
    expect(screen.getByLabelText("Ángulo FOV")).toHaveValue(90);
    expect(screen.getByLabelText("Alcance (m)")).toHaveValue(120);
  });

  it("rotates in 15° steps and wraps around the compass", () => {
    const onChange = vi.fn<(patch: Partial<DraftPlacement>) => void>();
    render(<PlacementPropsForm name="Recepción" draft={draft({ bearingDeg: 350 })} onChange={onChange} />);
    fireEvent.click(screen.getByRole("button", { name: "Girar 15° a la derecha" }));
    expect(onChange).toHaveBeenLastCalledWith({ bearingDeg: 5 });
    fireEvent.click(screen.getByRole("button", { name: "Girar 15° a la izquierda" }));
    expect(onChange).toHaveBeenLastCalledWith({ bearingDeg: 335 });
  });

  it("keeps the FOV inside the range the backend accepts", () => {
    const onChange = vi.fn<(patch: Partial<DraftPlacement>) => void>();
    render(<PlacementPropsForm name="Recepción" draft={draft()} onChange={onChange} />);
    fireEvent.change(screen.getByLabelText("Ángulo FOV"), { target: { value: "500" } });
    expect(onChange).toHaveBeenLastCalledWith({ fovDeg: 360 });
    fireEvent.change(screen.getByLabelText("Ángulo FOV"), { target: { value: "0" } });
    expect(onChange).toHaveBeenLastCalledWith({ fovDeg: 1 });
  });

  it("keeps the range non-negative", () => {
    const onChange = vi.fn<(patch: Partial<DraftPlacement>) => void>();
    render(<PlacementPropsForm name="Recepción" draft={draft()} onChange={onChange} />);
    fireEvent.change(screen.getByLabelText("Alcance (m)"), { target: { value: "-10" } });
    expect(onChange).toHaveBeenLastCalledWith({ rangeM: 0 });
  });
});
