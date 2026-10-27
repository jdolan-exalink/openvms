import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useState } from "react";
import { api, ApiError, type Schemas, unwrap } from "@/api/client";
import { cameraConfigQuery } from "@/api/queries";

export type ConfigPatch = Record<string, unknown>;
export type ConfigReview = Schemas["CameraConfigReview"];
export type ConfigApplyResult = Schemas["CameraConfigApplyResult"];
export type ConfigState = Schemas["CameraConfigState"];

export const STALE_REVISION_MESSAGE = "La configuración cambió mientras editabas.";

const isPlain = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

/** Deep-merges plain objects; arrays and scalars in `next` replace the previous value. */
export function mergePatch(base: ConfigPatch, next: ConfigPatch): ConfigPatch {
  const out: ConfigPatch = { ...base };
  for (const [k, v] of Object.entries(next)) out[k] = isPlain(v) && isPlain(out[k]) ? mergePatch(out[k], v) : v;
  return out;
}

/** Number of changed leaf fields in a sparse patch (an array counts as one field). */
export function countPatch(patch: ConfigPatch): number {
  return Object.values(patch).reduce<number>((n, v) => n + (isPlain(v) ? countPatch(v) : 1), 0);
}

/** Turns drift entries ("a.b" paths) into the sparse patch that restores the desired values. */
export function patchFromDrift(drift: ConfigState["drift"]): ConfigPatch {
  let out: ConfigPatch = {};
  for (const d of drift) {
    const nested = d.path.split(".").reduceRight<unknown>((acc, key) => ({ [key]: acc }), d.desired ?? null) as ConfigPatch;
    out = mergePatch(out, nested);
  }
  return out;
}

/**
 * Draft + review + apply for the normalized camera config. Tabs call `setPatch` with the fields
 * they edit; `review` and `apply` act on the accumulated draft. `apply(override)` sends an explicit
 * patch instead (re-applying the desired state) and leaves the draft untouched.
 */
export function useCameraConfigChange(cameraId: string) {
  const qc = useQueryClient();
  const config = useQuery(cameraConfigQuery(cameraId));
  const [patch, setPatchState] = useState<ConfigPatch>({});
  const [stale, setStale] = useState(false);

  const setPatch = useCallback((partial: ConfigPatch) => setPatchState((p) => mergePatch(p, partial)), []);
  const discard = useCallback(() => {
    setPatchState({});
    setStale(false);
  }, []);

  const review = useMutation({
    mutationFn: async (): Promise<ConfigReview> =>
      unwrap(await api.POST("/api/v1/cameras/{cameraId}/normalized-config/review", { params: { path: { cameraId } }, body: patch })),
  });

  const apply = useMutation({
    mutationFn: async (override?: ConfigPatch): Promise<ConfigApplyResult> => {
      const expected = config.data?.state.desired_revision ?? 0;
      const res = unwrap(
        await api.POST("/api/v1/cameras/{cameraId}/normalized-config/apply", {
          params: { path: { cameraId } },
          body: { patch: override ?? patch, expected_revision: expected, origin: "ui" },
        }),
      );
      return res;
    },
    onSuccess: async (res, override) => {
      setStale(false);
      if (res.result !== "error" && !override) setPatchState({});
      await qc.invalidateQueries({ queryKey: cameraConfigQuery(cameraId).queryKey });
    },
    onError: async (err) => {
      if (err instanceof ApiError && err.status === 409) {
        setStale(true); // the draft is kept; only the revision is refreshed
        await qc.invalidateQueries({ queryKey: cameraConfigQuery(cameraId).queryKey });
      }
    },
  });

  return {
    config,
    patch,
    setPatch,
    discard,
    review,
    apply,
    pendingCount: countPatch(patch),
    /** Set after a 409: the revision moved while editing. The draft is still there. */
    staleMessage: stale ? STALE_REVISION_MESSAGE : null,
  };
}

export type CameraConfigChange = ReturnType<typeof useCameraConfigChange>;
