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

  it("optional live hover attaches only through the shared surface and does not reconnect", () => {
    const connect = vi.spyOn(PlayerSession.prototype, "connect").mockImplementation(() => {});
    const attach = vi.spyOn(PlayerSession.prototype, "attach");
    const view = render(preview("prewarm", true), { wrapper });
    view.rerender(preview("live", true));
    expect(connect).toHaveBeenCalledTimes(1);
    expect(attach).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("video-surface-layer").querySelector("video")).not.toBeNull();
    expect(document.querySelector("[data-surface-slot='c1']")).not.toBeNull();
  });

  it("keeps another owner's video in the shared layer", async () => {
    vi.spyOn(PlayerSession.prototype, "connect").mockImplementation(() => {});
    function Owner() {
      const session = usePlayerSession("c1", "sub", "srv");
      return <SurfaceSlot session={session} />;
    }
    const view = render(<><Owner />{preview("prewarm", true)}</>, { wrapper });
    const video = screen.getByTestId("video-surface-layer").querySelector("video");
    const parent = video?.parentElement;
    await act(async () => view.rerender(<><Owner />{preview("live", true)}</>));
    expect(video?.parentElement).toBe(parent);
    expect(screen.getByTestId("video-surface-layer")).toContainElement(video);
  });

  it("renders nearby geo cameras but no fake actionable PTZ controls", () => {
    vi.spyOn(PlayerSession.prototype, "connect").mockImplementation(() => {});
    render(<CameraPanel pinnedCameras={[camera]} allCameras={[camera, {
      ...camera, id: "c2", name: "East", position: { kind: "geo", lat: 0, lng: 0.001 },
    }, { ...camera, id: "floor", name: "Floor", position: { kind: "floor", floorId: "f", x: 0, y: 0 } }]}
      onUnpin={() => {}} onSelectCamera={() => {}} onOpenLive={() => {}} />, { wrapper });
    expect(screen.getByText("Nearby Cameras (1)")).toBeInTheDocument();
    expect(screen.queryByTitle("PTZ controls")).not.toBeInTheDocument();
  });
});
