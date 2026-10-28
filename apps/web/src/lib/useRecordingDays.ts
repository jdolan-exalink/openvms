import { useQuery } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { recordingDaysQuery } from "@/api/queries";

const pad = (n: number) => String(n).padStart(2, "0");

/** dayKey is the local date of a day as the API names it ("2026-10-28"). */
export const dayKey = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

/**
 * useRecordingDays tells a day picker which days of the month it shows have video on the camera's server
 * or recorder. A failed or slow answer just leaves the days unmarked: the picker still works.
 */
export function useRecordingDays(cameraId: string | undefined, initial: Date) {
  const [month, setMonth] = useState({ year: initial.getFullYear(), month: initial.getMonth() });
  const from = new Date(month.year, month.month, 1);
  const to = new Date(month.year, month.month + 1, 1);
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const days = useQuery(recordingDaysQuery(cameraId ?? "", from.toISOString(), to.toISOString(), timezone));
  const set = useMemo(() => new Set(days.data ?? []), [days.data]);
  const marked = useCallback((d: Date) => set.has(dayKey(d)), [set]);
  const onMonthChange = useCallback((year: number, m: number) => setMonth((c) => (c.year === year && c.month === m ? c : { year, month: m })), []);
  return { marked, onMonthChange };
}
