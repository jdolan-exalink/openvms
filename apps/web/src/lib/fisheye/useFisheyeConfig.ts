import { useCallback, useSyncExternalStore } from "react";
import { fisheyeConfigStorageKey, getFisheyeConfig } from "./storage";
import type { FisheyeConfig } from "./types";

const CONFIG_CHANGED_EVENT = "openvms:fisheye-config-changed";

type CacheEntry = { raw: string | null; config: FisheyeConfig };
const cache = new Map<string, CacheEntry>();

function subscribe(onChange: () => void): () => void {
  window.addEventListener(CONFIG_CHANGED_EVENT, onChange);
  return () => window.removeEventListener(CONFIG_CHANGED_EVENT, onChange);
}

function readRaw(cameraId: string): string | null {
  try {
    return typeof localStorage !== "undefined" ? localStorage.getItem(fisheyeConfigStorageKey(cameraId)) : null;
  } catch {
    return null;
  }
}

/**
 * getFisheyeConfig parses and builds a fresh object on every call, so the snapshot is cached per
 * camera and keyed on the raw stored string: an unchanged string skips parsing and keeps the same
 * reference, while any write (with or without the change event) is picked up on the next read.
 */
function readSnapshot(cameraId: string): FisheyeConfig {
  const raw = readRaw(cameraId);
  const cached = cache.get(cameraId);
  if (cached && cached.raw === raw) return cached.config;
  const config = getFisheyeConfig(cameraId);
  cache.set(cameraId, { raw, config });
  return config;
}

/** Subscribes to the stored fisheye config of one camera. */
export function useFisheyeConfig(cameraId: string): FisheyeConfig {
  const getSnapshot = useCallback(() => readSnapshot(cameraId), [cameraId]);
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
