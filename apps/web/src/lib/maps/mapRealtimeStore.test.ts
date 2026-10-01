import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MapRealtimeStore } from "./mapRealtimeStore";
import type { CameraEntity } from "./types";

describe("MapRealtimeStore", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });


  it("can flush after cleanup and reconnect", () => {
    const store = new MapRealtimeStore(false);
    store.seedCameras([]);
    store.destroy();
    const flush = vi.fn();
    store.onCameraChange(flush);
    store.handleFrame({ type: "camera.status_changed", camera_id: "c", data: { to: "offline" } });
    vi.advanceTimersByTime(1000);
    expect(flush).toHaveBeenCalledOnce();
    store.destroy();
  });
  it("seeds REST alarm counts and handles repeated terminal updates idempotently", () => {
    const store = new MapRealtimeStore(false);
    store.seedCameras([{ id: "c", activeAlarms: 3 } as CameraEntity]);
    store.handleFrame({ type: "alarm.updated", camera_id: "c", data: { id: "a", status: "resolved" } });
    store.handleFrame({ type: "alarm.updated", camera_id: "c", data: { id: "a", status: "closed" } });
    expect(store.getCameraAlarms("c")).toBe(2);
    store.destroy();
  });
  it("patches a server-only outage and resets state on resync", () => {
    const store = new MapRealtimeStore(false);
    const cam = { id: "c", serverId: "srv", status: "online", activeAlarms: 0, metadata: {} } as CameraEntity;
    store.handleFrame({ type: "server.status", server_id: "srv", data: { status: "offline" } });
    expect(store.patchCameras([cam]).cameras[0]!.metadata.serverOffline).toBe(true);
    store.handleFrame({ op: "resync" });
    expect(store.isServerOffline("srv")).toBe(false);
    store.destroy();
  });
  it("rejects frames for another tenant", () => {
    const store = new MapRealtimeStore(false, "tenant-a");
    store.handleFrame({ tenant_id: "tenant-b", type: "camera.status_changed", camera_id: "c", data: { to: "offline" } });
    expect(store.getStatus("c")).toBeUndefined();
    store.destroy();
  });
  it("deduplicates frames with the same ID within 5-minute window", () => {
    const store = new MapRealtimeStore(false);
    let notifyCount = 0;
    store.subscribe(() => {
      notifyCount++;
    });

    const frame = {
      id: "evt-1",
      type: "camera.status_changed",
      camera_id: "cam-101",
      data: { to: "offline" },
    };

    store.handleFrame(frame);
    expect(notifyCount).toBe(1);
    expect(store.getStatus("cam-101")).toBe("offline");

    // Duplicate frame with same ID
    store.handleFrame(frame);
    expect(notifyCount).toBe(1);

    // After 5 minutes, same ID is no longer deduplicated
    vi.advanceTimersByTime(5 * 60 * 1000 + 1000);
    store.handleFrame(frame);
    expect(notifyCount).toBe(2);

    store.destroy();
  });

  it("handles camera status changes and normalizes states", () => {
    const store = new MapRealtimeStore(false);

    store.handleFrame({
      type: "camera.status_changed",
      camera_id: "c1",
      data: { to: "ONLINE" },
    });
    expect(store.getStatus("c1")).toBe("online");

    store.handleFrame({
      type: "camera.status_changed",
      camera_id: "c1",
      data: { to: "DEGRADED" },
    });
    expect(store.getStatus("c1")).toBe("degraded");

    store.handleFrame({
      type: "camera.status_changed",
      camera_id: "c1",
      data: { to: "OFFLINE" },
    });
    expect(store.getStatus("c1")).toBe("offline");

    store.destroy();
  });

  it("tracks server offline status", () => {
    const store = new MapRealtimeStore(false);

    expect(store.isServerOffline("srv-1")).toBe(false);

    store.handleFrame({
      type: "server.status_changed",
      server_id: "srv-1",
      data: { status: "offline" },
    });
    expect(store.isServerOffline("srv-1")).toBe(true);

    store.handleFrame({
      type: "server.status_changed",
      server_id: "srv-1",
      data: { status: "online" },
    });
    expect(store.isServerOffline("srv-1")).toBe(false);

    store.destroy();
  });

  it("tracks camera alarm counts on alarm.created and alarm.updated", () => {
    const store = new MapRealtimeStore(false);

    expect(store.getCameraAlarms("cam-1")).toBe(0);

    store.handleFrame({
      type: "alarm.created",
      camera_id: "cam-1",
    });
    expect(store.getCameraAlarms("cam-1")).toBe(1);

    store.handleFrame({
      type: "alarm.created",
      camera_id: "cam-1",
    });
    expect(store.getCameraAlarms("cam-1")).toBe(2);

    store.handleFrame({
      type: "alarm.updated",
      camera_id: "cam-1",
      data: { status: "resolved" },
    });
    expect(store.getCameraAlarms("cam-1")).toBe(1);

    store.destroy();
  });

  it("throttles cameraChangeCallbacks to at most 1 flush per second", () => {
    const store = new MapRealtimeStore(false);
    let flushCount = 0;
    store.onCameraChange(() => {
      flushCount++;
    });

    // Burst of 10 camera status changes in 100ms
    for (let i = 0; i < 10; i++) {
      store.handleFrame({
        type: "camera.status_changed",
        camera_id: `cam-${i}`,
        data: { to: "offline" },
      });
      vi.advanceTimersByTime(10);
    }

    // Still within throttle window
    expect(flushCount).toBe(0);

    // After 1000ms, exactly 1 flush should trigger
    vi.advanceTimersByTime(1000);
    expect(flushCount).toBe(1);

    // Another burst after 2 seconds
    vi.advanceTimersByTime(2000);
    store.handleFrame({
      type: "camera.status_changed",
      camera_id: "cam-99",
      data: { to: "online" },
    });
    vi.advanceTimersByTime(1000);
    expect(flushCount).toBe(2);

    store.destroy();
  });

  it("patches CameraEntity array with overrides and server offline status", () => {
    const store = new MapRealtimeStore(false);

    const initialCameras: CameraEntity[] = [
      {
        id: "cam-1",
        type: "camera",
        siteId: "site-1",
        serverId: "srv-1",
        name: "North Gate",
        position: { kind: "geo", lat: -34.6, lng: -58.38 },
        status: "online",
        activeAlarms: 0,
        camera: {
          bearingDeg: 90,
          fovDeg: 60,
          rangeM: 50,
          cameraType: "fixed",
          ptz: false,
          lpr: false,
        },
        metadata: { activeAlarms: 0 },
      },
    ];

    // No overrides yet
    const res1 = store.patchCameras(initialCameras);
    expect(res1.changed).toBe(false);
    expect(res1.cameras).toBe(initialCameras);

    // Add status override and alarm
    store.handleFrame({
      type: "camera.status_changed",
      camera_id: "cam-1",
      data: { to: "offline" },
    });
    store.handleFrame({
      type: "alarm.created",
      camera_id: "cam-1",
    });

    const res2 = store.patchCameras(initialCameras);
    expect(res2.changed).toBe(true);
    expect(res2.cameras[0]?.status).toBe("offline");
    expect(res2.cameras[0]?.metadata?.activeAlarms).toBe(1);

    store.destroy();
  });

  it("maintains 5-minute ring buffer of recent events", () => {
    const store = new MapRealtimeStore(false);

    store.handleFrame({
      id: "evt-10",
      type: "event.created",
      data: { label: "person" },
    });
    store.handleFrame({
      id: "evt-11",
      type: "event.created",
      data: { label: "car" },
    });

    const recent = store.getRecentEvents();
    expect(recent).toHaveLength(2);
    expect(recent[0]?.id).toBe("evt-11"); // reverse chronological

    // Expire items older than 5 minutes
    vi.advanceTimersByTime(6 * 60 * 1000);
    expect(store.getRecentEvents()).toHaveLength(0);

    store.destroy();
  });
});
