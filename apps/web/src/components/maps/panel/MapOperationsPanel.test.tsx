import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MapOperationsPanel } from "./MapOperationsPanel";
import type { ComponentProps } from "react";

const site = { id: "s", name: "Unlocated site", cameraCount: 2,
  health: { online: 1, offline: 1, degraded: 0, activeAlarms: 0, severity: "WARNING" as const } };
function setup(overrides: Partial<ComponentProps<typeof MapOperationsPanel>> = {}) {
  const props: ComponentProps<typeof MapOperationsPanel> = {
    mode: "live", sites: [site], cameras: [], visibleCount: 0, camerasVisible: true,
    canEdit: true, canLive: true, canEvents: true, canPlayback: true, canInventory: true,
    loading: false, errors: [], onSelectSite: vi.fn(), onSelectCamera: vi.fn(),
    onEdit: vi.fn(), onOpenLive: vi.fn(), onEvents: vi.fn(), onPlayback: vi.fn(), onResetVisibility: vi.fn(),
    ...overrides,
  };
  const view = render(<MapOperationsPanel {...props} />);
  return Object.assign(props, { unmount: view.unmount });
}
describe("Map operational states", () => {
  it("selects a coordinate-less site without relying on its map marker", () => {
    const props = setup();
    fireEvent.click(screen.getByRole("button", { name: /Unlocated site/ }));
    expect(props.onSelectSite).toHaveBeenCalledWith("s");
  });
  it("explains inventory versus placement", () => {
    setup({ currentSite: site });
    expect(screen.getByLabelText("Find camera")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Open placement editor" })).not.toBeInTheDocument();
  });
  it("retries failed placement data rather than reporting empty inventory", () => {
    const retry = vi.fn();
    setup({ currentSite: site, errors: [{ label: "Camera placements", error: new Error("Forbidden"), retry }] });
    expect(screen.getByRole("alert")).toHaveTextContent("Forbidden");
    expect(screen.queryByText(/ubicación guardada/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry camera placements" }));
    expect(retry).toHaveBeenCalledOnce();
  });
  it("recovers hidden cameras without altering inventory authorization", () => {
    const props = setup({ currentSite: site, cameras: [{ id: "c", name: "Camera", type: "camera", siteId: "s",
      position: { kind: "geo", lat: 1, lng: 2 }, status: "online", metadata: {}, activeAlarms: 0,
      camera: { bearingDeg: 0, fovDeg: 60, rangeM: 10, cameraType: "fixed", ptz: false, lpr: false } }], camerasVisible: false });
    fireEvent.click(screen.getByRole("button", { name: "Show all authorized markers" }));
    expect(props.onResetVisibility).toHaveBeenCalledOnce();
  });
  it("offers permission-aware camera-scoped investigation instead of live", () => {
    const camera = { id: "c", name: "Entrance", type: "camera" as const, siteId: "s",
      position: { kind: "geo" as const, lat: 1, lng: 2 }, status: "online" as const, metadata: {}, activeAlarms: 0,
      camera: { bearingDeg: 0, fovDeg: 60, rangeM: 10, cameraType: "fixed" as const, ptz: false, lpr: false } };
    const props = setup({ mode: "investigate", currentSite: site, cameras: [camera], selectedCameraId: "c", canPlayback: false });
    fireEvent.click(screen.getByRole("button", { name: "Open camera events" }));
    expect(props.onEvents).toHaveBeenCalledWith("c");
    expect(screen.queryByRole("button", { name: "Open camera playback" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Maximizar:/ })).not.toBeInTheDocument();
  });
  it("labels analytics as current summaries, not fabricated historical heatmaps", () => {
    setup({ mode: "analytics", currentSite: site });
    expect(screen.getByText(/not historical analytics or a heatmap/)).toBeInTheDocument();
    expect(screen.getByText("Inventory (overview)")).toBeInTheDocument();
  });
  it("distinguishes an invalid or unauthorized deep-link site", () => {
    setup({ requestedSiteId: "missing" });
    expect(screen.getByRole("alert")).toHaveTextContent("unavailable or not authorized");
  });
  describe("camera explorer rows", () => {
    const inventory = [
      { id: "a", display_name: "Placed cam", status: "online", server_id: "srv", folder_id: null },
      { id: "b", display_name: "Loose cam", status: "offline", server_id: "srv", folder_id: null },
    ] as never;
    const placed = { id: "a", name: "Placed cam", type: "camera" as const, siteId: "s",
      position: { kind: "geo" as const, lat: 1, lng: 2 }, status: "online" as const, metadata: {}, activeAlarms: 0,
      camera: { bearingDeg: 0, fovDeg: 60, rangeM: 10, cameraType: "fixed" as const, ptz: false, lpr: false } };

    it("marks only placed cameras with an on-map icon and no text label", () => {
      setup({ currentSite: site, cameras: [placed], inventory, servers: [{ id: "srv", name: "Server" }] });
      expect(screen.getAllByRole("img", { name: "En el mapa" })).toHaveLength(1);
      expect(screen.queryByText("En el mapa")).not.toBeInTheDocument();
    });
    it("toggles the live window with a pressed state and an accurate name", () => {
      const onOpenLive = vi.fn();
      const onCloseLive = vi.fn();
      const view = setup({ currentSite: site, cameras: [placed], inventory, servers: [{ id: "srv", name: "Server" }], openCameraIds: [], onOpenLive, onCloseLive });
      const open = screen.getByRole("button", { name: "Abrir vista en vivo: Placed cam" });
      expect(open).toHaveAttribute("aria-pressed", "false");
      fireEvent.click(open);
      expect(onOpenLive).toHaveBeenCalledWith("a");
      view.unmount();
      setup({ currentSite: site, cameras: [placed], inventory, servers: [{ id: "srv", name: "Server" }], openCameraIds: ["a"], onOpenLive, onCloseLive });
      const close = screen.getByRole("button", { name: "Cerrar vista en vivo: Placed cam" });
      expect(close).toHaveAttribute("aria-pressed", "true");
      fireEvent.click(close);
      expect(onCloseLive).toHaveBeenCalledWith("a");
      expect(screen.queryByRole("button", { name: /vista en vivo: Loose cam/ })).not.toBeInTheDocument();
    });
  });
});
