import { useInfiniteQuery, useQueries } from "@tanstack/react-query";
import { useEffect, useMemo } from "react";
import { eventsQuery, recordingsQuery } from "@/api/queries";
import { ApiError } from "@/api/client";
import { vehicleHeadline, vehiclePaint } from "@/lib/format";
import { recorderMotionEvents } from "@/lib/recorderMotion";
import { DAY_S, mergeSpans, type Span } from "@/lib/timeScale";
import { useRecorderCameraIds } from "@/lib/useRecorderCameras";

/** Event pages (500 each) fetched for the timeline markers; more days are dense enough to cluster anyway. */
const MAX_EVENT_PAGES = 4;

type Coverage = { spans: Record<string, Span[]>; denied: string[]; loaded: string[]; motion: ReturnType<typeof recorderMotionEvents> };

/**
 * useRecData loads, for the cameras of the grid and one local day, the recording coverage of
 * each camera (a 403 marks the camera as lacking recordings permission) and the day's events.
 */
export function useRecData(cameraIds: string[], day: number, enabled: boolean) {
  const from = new Date(day * 1000).toISOString();
  const to = new Date((day + DAY_S) * 1000).toISOString();
  const recorders = useRecorderCameraIds();

  const coverage = useQueries({
    queries: cameraIds.map((id) => ({
      ...recordingsQuery(id, from, to),
      enabled,
      refetchInterval: 60_000,
      retry: (count: number, error: unknown) => !(error instanceof ApiError && error.status === 403) && count < 2,
    })),
    combine: (results): Coverage => {
      const out: Coverage = { spans: {}, denied: [], loaded: [], motion: [] };
      results.forEach((r, i) => {
        const id = cameraIds[i]!;
        if (r.data) {
          out.loaded.push(id);
          // A recorder has no analytics of its own: its motion detections are what the timeline can show.
          if (recorders.has(id)) out.motion.push(...recorderMotionEvents(id, r.data));
          out.spans[id] = mergeSpans(r.data.map((s) => ({ start: Date.parse(s.start_time) / 1000, end: Date.parse(s.end_time) / 1000 })));
        } else if (r.error instanceof ApiError && r.error.status === 403) {
          out.denied.push(id);
        }
      });
      return out;
    },
  });

  const events = useInfiniteQuery({
    ...eventsQuery({ camera_id: cameraIds, from, to, limit: 500 }),
    enabled: enabled && cameraIds.length > 0,
  });
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = events;
  const pageCount = events.data?.pages.length ?? 0;
  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage && pageCount < MAX_EVENT_PAGES) void fetchNextPage();
  }, [hasNextPage, isFetchingNextPage, pageCount, fetchNextPage]);
  const eventList = useMemo(
    () =>
      (events.data?.pages.flatMap((p) => p.items) ?? [])
        .map((e) => {
          const time = Date.parse(e.start_time) / 1000;
          const end = e.end_time ? Date.parse(e.end_time) / 1000 : undefined;
          return {
            id: e.id,
            time,
            end: end !== undefined && end > time ? end : undefined,
            severity: e.severity as string,
            cameraId: e.camera_id,
            label: e.labels[0] ?? "",
            detail: e.plates?.[0] || e.sub_labels?.[0] || undefined,
            color: e.labels.some((label) => label === "car" || label === "motorcycle") ? vehiclePaint(e.attributes?.vehicle) ?? undefined : undefined,
            title: vehicleHeadline(e.attributes?.vehicle) ?? undefined,
          };
        })
        .sort((a, b) => a.time - b.time),
    [events.data],
  );

  const withMotion = useMemo(
    () =>
      coverage.motion.length === 0
        ? eventList
        : [
            ...eventList,
            ...coverage.motion.map((m) => ({
              id: m.id,
              time: Date.parse(m.start_time) / 1000,
              end: Date.parse(m.end_time) / 1000,
              severity: m.severity,
              cameraId: m.camera_id,
              label: m.labels[0] ?? "",
              detail: undefined,
              color: undefined,
              title: undefined,
            })),
          ].sort((a, b) => a.time - b.time),
    [eventList, coverage.motion],
  );

  return { spans: coverage.spans, denied: coverage.denied, loaded: coverage.loaded, events: withMotion, error: events.error };
}
