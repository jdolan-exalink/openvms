import type { CameraEntity, MapEntity } from "./types";
import { subscribeFrames } from "../realtime";

export interface RealtimeFrame {
  v?: number;
  id?: string;
  type?: string;
  ts?: string | number;
  tenant_id?: string;
  site_id?: string;
  camera_id?: string;
  server_id?: string;
  data?: unknown;
  op?: string;
  frames?: unknown[];
}

export interface RealtimeEvent {
  id: string;
  ts: number;
  type: string;
  siteId?: string;
  cameraId?: string;
  serverId?: string;
  data: unknown;
}

export interface MapRealtimeSnapshot {
  statusOverrides: ReadonlyMap<string, MapEntity["status"]>;
  serverOffline: ReadonlySet<string>;
  cameraAlarms: ReadonlyMap<string, number>;
  recentEventsCount: number;
  revision: number;
}

const FIVE_MINUTES_MS = 5 * 60 * 1000;
const MAX_RING_BUFFER_SIZE = 20_000;
const MIN_FLUSH_INTERVAL_MS = 1_000; // at most 1/s throttled camera updates per ARCHITECTURE.md §10.3

export class MapRealtimeStore {
  private statusOverrides = new Map<string, { status: MapEntity["status"]; at: number }>();
  private serverOffline = new Set<string>();
  private cameraAlarms = new Map<string, number>();
  private ringBuffer: RealtimeEvent[] = [];
  private seenIds = new Map<string, number>(); // id -> timestamp
  private listeners = new Set<() => void>();
  private cameraChangeCallbacks = new Set<() => void>();

  private revision = 0;
  private currentSnapshot: MapRealtimeSnapshot;
  private unsubscribeFrames: (() => void) | null = null;

  private pendingCameraFlush = false;
  private lastCameraFlushTime = Date.now();
  private cameraFlushTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(autoSubscribe = true) {
    this.currentSnapshot = this.buildSnapshot();
    if (autoSubscribe) {
      this.unsubscribeFrames = subscribeFrames((frame) => this.handleFrame(frame));
    }
  }

  destroy(): void {
    if (this.unsubscribeFrames) {
      this.unsubscribeFrames();
      this.unsubscribeFrames = null;
    }
    if (this.cameraFlushTimer) {
      clearTimeout(this.cameraFlushTimer);
      this.cameraFlushTimer = null;
    }
    this.listeners.clear();
    this.cameraChangeCallbacks.clear();
  }

  private buildSnapshot(): MapRealtimeSnapshot {
    const overrides = new Map<string, MapEntity["status"]>();
    for (const [id, val] of this.statusOverrides.entries()) {
      overrides.set(id, val.status);
    }

    return {
      statusOverrides: overrides,
      serverOffline: new Set(this.serverOffline),
      cameraAlarms: new Map(this.cameraAlarms),
      recentEventsCount: this.ringBuffer.length,
      revision: this.revision,
    };
  }

  private notify(): void {
    this.revision++;
    this.currentSnapshot = this.buildSnapshot();
    for (const listener of this.listeners) {
      try {
        listener();
      } catch {
        // Listener error should not break store
      }
    }
  }

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  /**
   * Subscribe to camera status changes (throttled at most 1/s for GeoJSON setData)
   */
  onCameraChange(callback: () => void): () => void {
    this.cameraChangeCallbacks.add(callback);
    return () => {
      this.cameraChangeCallbacks.delete(callback);
    };
  }

  getSnapshot = (): MapRealtimeSnapshot => {
    return this.currentSnapshot;
  };

  getStatus(cameraId: string): MapEntity["status"] | undefined {
    return this.statusOverrides.get(cameraId)?.status;
  }

  isServerOffline(serverId: string): boolean {
    return this.serverOffline.has(serverId);
  }

  getCameraAlarms(cameraId: string): number {
    return this.cameraAlarms.get(cameraId) ?? 0;
  }

  getRecentEvents(limit = 100): RealtimeEvent[] {
    const now = Date.now();
    this.pruneExpired(now);
    return this.ringBuffer.slice(-limit).reverse();
  }

  private pruneExpired(now: number): void {
    const cutoff = now - FIVE_MINUTES_MS;

    // Prune seen IDs
    for (const [id, ts] of this.seenIds.entries()) {
      if (ts < cutoff) {
        this.seenIds.delete(id);
      }
    }

    // Prune ring buffer
    let dropCount = 0;
    while (dropCount < this.ringBuffer.length && this.ringBuffer[dropCount]!.ts < cutoff) {
      dropCount++;
    }
    if (dropCount > 0) {
      this.ringBuffer.splice(0, dropCount);
    }
  }

  handleFrame(raw: unknown): void {
    if (!raw || typeof raw !== "object") return;
    const frame = raw as RealtimeFrame;

    // Handle batch frames
    if (frame.op === "batch" && Array.isArray(frame.frames)) {
      for (const sub of frame.frames) {
        this.handleFrame(sub);
      }
      return;
    }

    const now = Date.now();
    this.pruneExpired(now);

    // Dedup check (5-minute TTL)
    if (frame.id) {
      if (this.seenIds.has(frame.id)) {
        return; // Duplicate frame, ignore
      }
      this.seenIds.set(frame.id, now);
    }

    const type = frame.type;
    let cameraAffected = false;

    if (type === "camera.status_changed") {
      const camId = frame.camera_id || (frame.data as Record<string, unknown>)?.camera_id;
      const data = frame.data as { to?: string } | undefined;
      if (typeof camId === "string" && data?.to) {
        const rawStatus = data.to.toLowerCase();
        let normalizedStatus: MapEntity["status"] = "unknown";
        if (rawStatus === "online") normalizedStatus = "online";
        else if (rawStatus === "offline" || rawStatus === "unreachable") normalizedStatus = "offline";
        else if (rawStatus === "degraded" || rawStatus === "no_signal" || rawStatus === "recording_error") normalizedStatus = "degraded";

        this.statusOverrides.set(camId, { status: normalizedStatus, at: now });
        cameraAffected = true;
      }
    } else if (type === "server.status" || type === "server.status_changed") {
      const serverId = frame.server_id || (frame.data as Record<string, unknown>)?.server_id;
      const data = frame.data as { status?: string } | undefined;
      if (typeof serverId === "string" && data?.status) {
        if (data.status.toLowerCase() === "offline") {
          this.serverOffline.add(serverId);
        } else if (data.status.toLowerCase() === "online") {
          this.serverOffline.delete(serverId);
        }
        cameraAffected = true;
      }
    } else if (type === "alarm.created") {
      const camId = frame.camera_id || (frame.data as Record<string, unknown>)?.camera_id;
      if (typeof camId === "string") {
        const current = this.cameraAlarms.get(camId) ?? 0;
        this.cameraAlarms.set(camId, current + 1);
        cameraAffected = true;
      }
    } else if (type === "alarm.updated" || type === "alarm.acknowledged") {
      const camId = frame.camera_id || (frame.data as Record<string, unknown>)?.camera_id;
      const data = frame.data as { status?: string } | undefined;
      if (typeof camId === "string" && data?.status) {
        const s = data.status.toLowerCase();
        if (s === "resolved" || s === "closed") {
          const current = this.cameraAlarms.get(camId) ?? 0;
          if (current > 0) {
            this.cameraAlarms.set(camId, current - 1);
            cameraAffected = true;
          }
        }
      }
    }

    // Add to ring buffer
    if (type) {
      const event: RealtimeEvent = {
        id: frame.id || `evt-${now}-${Math.random().toString(36).slice(2, 8)}`,
        ts: typeof frame.ts === "number" ? frame.ts : typeof frame.ts === "string" ? new Date(frame.ts).getTime() : now,
        type,
        siteId: frame.site_id,
        cameraId: frame.camera_id,
        serverId: frame.server_id,
        data: frame.data,
      };

      this.ringBuffer.push(event);
      if (this.ringBuffer.length > MAX_RING_BUFFER_SIZE) {
        this.ringBuffer.shift();
      }
    }

    this.notify();

    if (cameraAffected) {
      this.scheduleCameraFlush();
    }
  }

  private scheduleCameraFlush(): void {
    if (this.pendingCameraFlush) return;
    this.pendingCameraFlush = true;

    const now = Date.now();
    const elapsed = now - this.lastCameraFlushTime;
    const delay = Math.max(0, MIN_FLUSH_INTERVAL_MS - elapsed);

    if (this.cameraFlushTimer) {
      clearTimeout(this.cameraFlushTimer);
    }

    this.cameraFlushTimer = setTimeout(() => {
      this.pendingCameraFlush = false;
      this.lastCameraFlushTime = Date.now();
      for (const cb of this.cameraChangeCallbacks) {
        try {
          cb();
        } catch {
          // Ignore callback error
        }
      }
    }, delay);
  }

  /**
   * Patches a list of CameraEntity objects with live status overrides, active alarms,
   * and server offline state. Returns a new array if any changes occurred.
   */
  patchCameras(cameras: CameraEntity[]): { cameras: CameraEntity[]; changed: boolean } {
    let changed = false;

    const patched = cameras.map((cam) => {
      const statusOverride = this.statusOverrides.get(cam.id);
      const isServerDown = cam.serverId ? this.serverOffline.has(cam.serverId) : false;
      const liveAlarms = this.cameraAlarms.get(cam.id);

      const targetStatus = statusOverride ? statusOverride.status : cam.status;
      const currentAlarms = cam.activeAlarms ?? 0;
      const targetAlarms = liveAlarms !== undefined ? liveAlarms : currentAlarms;

      if (targetStatus !== cam.status || targetAlarms !== currentAlarms) {
        changed = true;
        return {
          ...cam,
          status: targetStatus,
          activeAlarms: targetAlarms,
          metadata: {
            ...cam.metadata,
            activeAlarms: targetAlarms,
            serverOffline: isServerDown,
          },
        };
      }
      return cam;
    });

    return { cameras: changed ? patched : cameras, changed };
  }
}

// Global default singleton for maps
export const defaultMapRealtimeStore = new MapRealtimeStore();
