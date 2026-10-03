import { describe, expect, it } from "vitest";
import type { PlayerSession, SessionSnapshot } from "./PlayerSession";
import { VideoSurfaceLayerController } from "./surfaceLayer";

function fakeSession(state: SessionSnapshot["state"] = "ACTIVE") {
  const video = document.createElement("video");
  const listeners = new Set<() => void>();
  let snapshot: SessionSnapshot = { state, message: "", error: null, poster: null };
  const session = {
    cameraId: "cam-1",
    video,
    attach: (el: HTMLElement) => el.appendChild(video),
    getSnapshot: () => snapshot,
    subscribe: (l: () => void) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
  };
  return {
    session: session as unknown as PlayerSession,
    video,
    setState: (s: SessionSnapshot["state"]) => {
      snapshot = { ...snapshot, state: s };
      listeners.forEach((l) => l());
    },
  };
}

function slotAt(rect: { left: number; top: number; width: number; height: number }) {
  const slot = document.createElement("div");
  document.body.appendChild(slot);
  slot.getBoundingClientRect = () =>
    ({ ...rect, x: rect.left, y: rect.top, right: rect.left + rect.width, bottom: rect.top + rect.height, toJSON: () => ({}) }) as DOMRect;
  return slot;
}

describe("VideoSurfaceLayerController", () => {
  it("refuses an exclusive Maps slot while another owner is registered", () => {
    const layer = new VideoSurfaceLayerController();
    const { session } = fakeSession();
    const original = slotAt({ left: 10, top: 20, width: 100, height: 100 });
    const release = layer.register(session, original);
    const other = slotAt({ left: 300, top: 20, width: 100, height: 100 });
    const releaseOther = layer.register(session, other, true);
    releaseOther();
    const host = document.createElement("div");
    layer.setHost(host);
    expect(host.firstElementChild?.getAttribute("style")).toContain("translate3d(10px");
    release();
  });

  it("positions the persistent video over its slot and never re-parents it", () => {
    const host = document.createElement("div");
    document.body.appendChild(host);
    const layer = new VideoSurfaceLayerController();
    layer.setHost(host);
    const { session, video } = fakeSession();

    const first = slotAt({ left: 10, top: 20, width: 300, height: 200 });
    const release = layer.register(session, first);
    const stage = video.parentElement as HTMLElement;
    const wrapper = stage.parentElement as HTMLElement;
    expect(wrapper.parentElement).toBe(host);
    expect(wrapper.style.transform).toBe("translate3d(10px, 20px, 0)");
    expect(wrapper.style.width).toBe("300px");
    expect(wrapper.style.visibility).toBe("visible");
    expect(stage.getAttribute("data-video-zoom")).toBe("cam-1");

    // The cell moved (drag and drop / layout / expand): only the transform changes.
    release();
    expect(wrapper.style.visibility).toBe("hidden");
    layer.register(session, slotAt({ left: 400, top: 300, width: 600, height: 400 }));
    expect(wrapper.style.transform).toBe("translate3d(400px, 300px, 0)");
    expect(wrapper.style.height).toBe("400px");
    expect(video.parentElement?.parentElement).toBe(wrapper);
    expect(wrapper.parentElement).toBe(host);
  });

  it("hides the video while the session is not playing so the poster shows through, and drops evicted sessions", () => {
    const host = document.createElement("div");
    document.body.appendChild(host);
    const layer = new VideoSurfaceLayerController();
    layer.setHost(host);
    const { session, video, setState } = fakeSession("RECONNECTING");
    layer.register(session, slotAt({ left: 0, top: 0, width: 100, height: 100 }));
    const wrapper = video.parentElement?.parentElement as HTMLElement;
    expect(wrapper.style.visibility).toBe("hidden");
    setState("ACTIVE");
    expect(wrapper.style.visibility).toBe("visible");
    setState("EVICTED");
    expect(host.contains(wrapper)).toBe(false);
  });

  it("zooms an inner stage and leaves the video element without a transform", () => {
    const host = document.createElement("div");
    document.body.appendChild(host);
    const layer = new VideoSurfaceLayerController();
    layer.setHost(host);
    const { session, video } = fakeSession();
    layer.register(session, slotAt({ left: 8, top: 12, width: 160, height: 90 }));
    layer.setPictureZoom(session, { scale: 2, x: -10, y: -20 });
    const stage = video.parentElement as HTMLElement;
    expect(stage.style.transform).toBe("translate(-10px, -20px) scale(2)");
    expect(video.style.transform).toBe("");
    expect(stage.parentElement?.style.transform).toBe("translate3d(8px, 12px, 0)");
    layer.setPictureZoom(session, { scale: 1, x: 0, y: 0 });
    expect(stage.style.transform).toBe("");
  });
});
