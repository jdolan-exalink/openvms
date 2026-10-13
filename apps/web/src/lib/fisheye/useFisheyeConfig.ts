import { useCallback, useSyncExternalStore } from "react";
import { getFisheyeConfig } from "./storage";
import type { FisheyeConfig } from "./types";

const CONFIG_CHANGED_EVENT = "openvms:fisheye-config-changed";

type CacheEntry = { serialized: string; config: FisheyeConfig };
const cache = new Map<string, CacheEntry>();

function subscribe(onChange: () => void): () => void {
  window.addEventListener(CONFIG_CHANGED_EVENT, onChange);
  return () => window.removeEventListener(CONFIG_CHANGED_EVENT, onChange);
}

/**
 * getFisheyeConfig builds a fresh object on every call, so the snapshot is
 * cached per camera and only replaced when the serialized config changes.
 */
function readSnapshot(cameraId: string): FisheyeConfig {
  const config = getFisheyeConfig(cameraId);
  const serialized = JSON.stringify(config);
  const cached = cache.get(cameraId);
  if (cached && cached.serialized === serialized) return cached.config;
  cache.set(cameraId, { serialized, config });
  return config;
}

/** Subscribes to the stored fisheye config of one camera. */
export function useFisheyeConfig(cameraId: string): FisheyeConfig {
  const getSnapshot = useCallback(() => readSnapshot(cameraId), [cameraId]);
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
