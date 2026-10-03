import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CameraPreview } from "./CameraPreview";
import { CameraPanel } from "./CameraPanel";
import { PlayerSessionProvider, usePlayerSession } from "@/lib/live/PlayerSessionProvider";
import { PlayerSession } from "@/lib/live/PlayerSession";
import { PlayerSessionManager } from "@/lib/live/PlayerSessionManager";
import { SurfaceSlot, VideoSurfaceLayer } from "@/lib/live/SurfaceLayer";
import type { CameraEntity } from "@/lib/maps/types";

const camera: CameraEntity = {
  id: "c1", type: "camera", siteId: "s", serverId: "srv", name: "North",
  position: { kind: "geo", lat: 0, lng: 0 }, status: "online", metadata: {}, activeAlarms: 0,
  camera: { bearingDeg: 0, fovDeg: 60, rangeM: 100, cameraType: "ptz", ptz: true, lpr: false },
};
afterEach(() => vi.restoreAllMocks());

function wrapper({ children }: { children: React.ReactNode }) {
  return <PlayerSessionProvider><VideoSurfaceLayer>{children}</VideoSurfaceLayer></PlayerSessionProvider>;
}
const preview = (stage: "tooltip" | "snapshot" | "prewarm" | "live", liveOnHover = false) =>
  <CameraPreview camera={camera} stage={stage} position={{ x: 10, y: 20 }} liveOnHover={liveOnHover} />;

describe("Maps shared preview ownership", () => {

  it("shows camera B after camera A snapshot fails", () => {
    const view = render(preview("snapshot"), { wrapper });
    fireEvent.error(screen.getByAltText("North"));
    expect(screen.getByAltText("North")).not.toBeVisible();
    view.rerender(<CameraPreview camera={{ ...camera, id: "c2", name: "East" }}
      stage="snapshot" position={{ x: 10, y: 20 }} />);
    const image = screen.getByAltText("East");
    fireEvent.load(image);
    expect(image).toBeVisible();
    expect(image).toHaveAttribute("src", "/media/v1/cameras/c2/snapshot.jpg?h=240");
  });

  it("does not acquire an empty ID during tooltip or snapshot", () => {
    const acquire = vi.spyOn(PlayerSessionManager.prototype, "acquire");
    vi.spyOn(PlayerSession.prototype, "connect").mockImplementation(() => {});
    const view = render(preview("tooltip"), { wrapper });
    view.rerender(preview("snapshot"));
    expect(acquire).not.toHaveBeenCalled();
  });

  it("prewarms without attaching video, then releases exactly once", () => {
    vi.spyOn(PlayerSession.prototype, "connect").mockImplementation(() => {});
    const acquire = vi.spyOn(PlayerSessionManager.prototype, "acquire");
    const release = vi.spyOn(PlayerSessionManager.prototype, "release");
    const attach = vi.spyOn(PlayerSession.prototype, "attach");
    const view = render(preview("snapshot"), { wrapper });
    view.rerender(preview("prewarm"));
    expect(acquire).toHaveBeenCalledTimes(1);
    expect(acquire).toHaveBeenCalledWith("c1", "sub", "srv");
    expect(attach).not.toHaveBeenCalled();
    view.unmount();
    expect(release).toHaveBeenCalledTimes(1);
    expect(release).toHaveBeenCalledWith("c1", "sub");
  });

  it("optional live hover plays inside the card and does not reconnect", () => {
    const connect = vi.spyOn(PlayerSession.prototype, "connect").mockImplementation(() => {});
    const attach = vi.spyOn(PlayerSession.prototype, "attach");
    const view = render(<CameraPreview camera={camera} stage="prewarm" position={{ x: 10, y: 20 }} liveOnHover persistent />, { wrapper });
    view.rerender(<CameraPreview camera={camera} stage="live" position={{ x: 10, y: 20 }} liveOnHover persistent />);
    expect(connect).toHaveBeenCalledTimes(1);
    expect(attach).toHaveBeenCalled();
    const card = screen.getByTestId("camera-hover-preview");
    expect(card.querySelector("video")).not.toBeNull();
    expect(screen.getByTestId("video-surface-layer").querySelector("video")).toBeNull();
    expect(screen.getByAltText("North")).toBeInTheDocument();
  });

  it("returns a borrowed video to the other owner when the hover closes", async () => {
    vi.spyOn(PlayerSession.prototype, "connect").mockImplementation(() => {});
    function Owner() {
      const session = usePlayerSession("c1", "sub", "srv");
      return <SurfaceSlot session={session} />;
    }
    const live = <CameraPreview camera={camera} stage="live" position={{ x: 10, y: 20 }} liveOnHover persistent />;
    const view = render(<><Owner />{live}</>, { wrapper });
    const card = screen.getByTestId("camera-hover-preview");
    const video = card.querySelector("video");
    expect(video).not.toBeNull();
    await act(async () => view.rerender(<Owner />));
    expect(screen.getByTestId("video-surface-layer")).toContainElement(video);
  });

  it("renders the compact preview without nearby cameras or the live grid action", () => {
    vi.spyOn(PlayerSession.prototype, "connect").mockImplementation(() => {});
    render(<CameraPanel pinnedCameras={[camera]} onUnpin={() => {}} onOpenLive={() => {}} />, { wrapper });
    expect(screen.queryByText(/Nearby Cameras/)).not.toBeInTheDocument();
    expect(screen.queryByText("Live Grid")).not.toBeInTheDocument();
    expect(screen.queryByText("PTZ")).not.toBeInTheDocument();
  });

  it("drags a preview from its header and reports the new place", () => {
    vi.spyOn(PlayerSession.prototype, "connect").mockImplementation(() => {});
    const onMove = vi.fn();
    render(<CameraPanel pinnedCameras={[camera]} windows={[{ id: "c1", x: 40, y: 50 }]} onUnpin={() => {}} onOpenLive={() => {}} onMove={onMove} />, { wrapper });
    const layer = screen.getByTestId("pinned-windows");
    vi.spyOn(layer, "getBoundingClientRect").mockReturnValue({
      left: 0, top: 0, width: 1000, height: 800, right: 1000, bottom: 800, x: 0, y: 0, toJSON() { return {}; },
    });
    fireEvent.pointerDown(screen.getByRole("heading", { name: "North" }), { clientX: 10, clientY: 10, pointerId: 1 });
    fireEvent.pointerUp(window, { clientX: 80, clientY: 40 });
    expect(onMove).toHaveBeenCalledWith("c1", 110, 80);
  });

  it("orders every open window from the header", () => {
    vi.spyOn(PlayerSession.prototype, "connect").mockImplementation(() => {});
    const onArrange = vi.fn();
    render(<CameraPanel pinnedCameras={[camera]} onUnpin={() => {}} onOpenLive={() => {}} onArrange={onArrange} />, { wrapper });
    fireEvent.click(screen.getByRole("button", { name: "Ordenar ventanas" }));
    expect(onArrange).toHaveBeenCalledTimes(1);
  });
});
