import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { featuresQuery } from "@/api/queries";

export type FeatureFlags = {
  persistentPlayers: boolean;
  videoSurfaceLayer: boolean;
  adaptiveStreaming: boolean;
  streamPrewarming: boolean;
  seamlessQualitySwitch: boolean;
  maps: boolean;
};

/** Every flag is off until the server says otherwise, so a failing endpoint keeps legacy behaviour. */
export const FEATURE_DEFAULTS: FeatureFlags = {
  persistentPlayers: false,
  videoSurfaceLayer: false,
  adaptiveStreaming: false,
  streamPrewarming: false,
  seamlessQualitySwitch: false,
  maps: false,
};

/** Development-only localStorage override, e.g. {"persistentPlayers":true}. Ignored in production builds. */
export const FEATURES_OVERRIDE_KEY = "openvms.features.override";

function readOverride(): Partial<FeatureFlags> {
  if (!import.meta.env.DEV) return {};
  try {
    const raw = localStorage.getItem(FEATURES_OVERRIDE_KEY);
    const parsed: unknown = raw ? JSON.parse(raw) : null;
    if (!parsed || typeof parsed !== "object") return {};
    const out: Partial<FeatureFlags> = {};
    for (const name of Object.keys(FEATURE_DEFAULTS) as (keyof FeatureFlags)[]) {
      const value = (parsed as Record<string, unknown>)[name];
      if (typeof value === "boolean") out[name] = value;
    }
    return out;
  } catch {
    return {};
  }
}

/** useFeatures returns the rollout flags, falling back to all-off when the endpoint is unavailable. */
export function useFeatures(): FeatureFlags {
  const { data } = useQuery(featuresQuery);
  return useMemo(
    () => ({
      persistentPlayers: data?.persistent_players ?? FEATURE_DEFAULTS.persistentPlayers,
      videoSurfaceLayer: data?.video_surface_layer ?? FEATURE_DEFAULTS.videoSurfaceLayer,
      adaptiveStreaming: data?.adaptive_streaming ?? FEATURE_DEFAULTS.adaptiveStreaming,
      streamPrewarming: data?.stream_prewarming ?? FEATURE_DEFAULTS.streamPrewarming,
      seamlessQualitySwitch: data?.seamless_quality_switch ?? FEATURE_DEFAULTS.seamlessQualitySwitch,
      maps: data?.maps ?? FEATURE_DEFAULTS.maps,
      ...readOverride(),
    }),
    [data],
  );
}
