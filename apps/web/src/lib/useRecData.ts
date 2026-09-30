import { useInfiniteQuery, useQueries } from "@tanstack/react-query";
import { useEffect, useMemo } from "react";
import { eventsQuery, recordingsQuery } from "@/api/queries";
import { ApiError } from "@/api/client";
import { DAY_S, mergeSpans, type Span } from "@/lib/timeScale";

/** Event pages (500 each) fetched for the timeline markers; more days are dense enough to cluster anyway. */
const MAX_EVENT_PAGES = 4;

type Coverage = { spans: Record<string, Span[]>; denied: string[]; loaded: string[] };

/**
 * useRecData loads, for the cameras of the grid and one local day, the recording coverage of
 * each camera (a 403 marks the camera as lacking recordings permission) and the day's events.
 */
export function useRecData(cameraIds: string[], day: number, enabled: boolean) {
  const from = new Date(day * 1000).toISOString();
  const to = new Date((day + DAY_S) * 1000).toISOString();

  const coverage = useQueries({
    queries: cameraIds.map((id) => ({
      ...recordingsQuery(id, from, to),
      enabled,
      refetchInterval: 60_000,
      retry: (count: number, error: unknown) => !(error instanceof ApiError && error.status === 403) && count < 2,
    })),
    combine: (results): Coverage => {
      const out: Coverage = { spans: {}, denied: [], loaded: [] };
      results.forEach((r, i) => {
        const id = cameraIds[i]!;
        if (r.data) {
          out.loaded.push(id);
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
        .map((e) => ({ time: Date.parse(e.start_time) / 1000, severity: e.severity as string }))
        .sort((a, b) => a.time - b.time),
    [events.data],
  );

  return { spans: coverage.spans, denied: coverage.denied, loaded: coverage.loaded, events: eventList, error: events.error };
}
