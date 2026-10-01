import type { CameraEntity } from "./types";

/** Incident focus is session-local until M-W7 adds persisted preferences. */
export class IncidentFocus {
  mode: "none" | "current-site" = "none";
  setMode(mode: "none" | "current-site"): void { this.mode = mode; }
  private lastInteraction = -Infinity;
  private lastMove = -Infinity;
  interact(now = Date.now()): void { this.lastInteraction = now; }
  accept(camera: CameraEntity, siteId: string | undefined, now = Date.now()): boolean {
    if (this.mode === "none" || camera.siteId !== siteId || camera.position.kind !== "geo") return false;
    if (now - this.lastInteraction < 15_000 || now - this.lastMove < 10_000) return false;
    this.lastMove = now;
    return true;
  }
}

export function groupSiteHealth(cameras: CameraEntity[]) {
  const groups = new Map<string, { id: string; kind: "server" | "camera"; name: string; cameraIds: string[] }>();
  for (const camera of cameras) {
    const serverDown = !!camera.metadata.serverOffline && !!camera.serverId;
    if (!serverDown && camera.status === "online") continue;
    const id = serverDown ? camera.serverId! : camera.id;
    const key = serverDown ? `server:${id}` : `camera:${id}`;
    const group = groups.get(key) ?? {
      id, kind: serverDown ? "server" as const : "camera" as const,
      name: serverDown ? `Server ${id} offline` : `${camera.name}: ${camera.status}`, cameraIds: [],
    };
    group.cameraIds.push(camera.id);
    groups.set(key, group);
  }
  return [...groups.values()];
}
